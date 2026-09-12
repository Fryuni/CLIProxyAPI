package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func transcriptionTestHandler(t *testing.T, baseURL string, compatible bool) *OpenAIAPIHandler {
	t.Helper()
	cfg := &internalconfig.Config{OpenAICompatibility: []internalconfig.OpenAICompatibility{{
		Name: "transcription-test", Models: []internalconfig.OpenAICompatibilityModel{{Name: "whisper-1", Alias: "speech-test"}},
	}}}
	m := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
	m.SetConfig(cfg)
	m.SetRetryConfig(3, 0, 0)
	m.RegisterExecutor(runtimeexecutor.NewCodexExecutor(cfg))
	providers := []string{"codex"}
	if compatible {
		m.RegisterExecutor(runtimeexecutor.NewOpenAICompatExecutor(util.OpenAICompatibleProviderKey("transcription-test"), cfg))
		providers = append(providers, util.OpenAICompatibleProviderKey("transcription-test"), util.OpenAICompatibleProviderKey("transcription-test"))
	}
	for i, provider := range providers {
		auth := &coreauth.Auth{ID: fmt.Sprintf("%s-%d", t.Name(), i), Provider: provider, Status: coreauth.StatusActive,
			Attributes: map[string]string{"base_url": baseURL, "api_key": "test-key", "priority": "100"}}
		if provider != "codex" {
			auth.Attributes["compat_name"] = "transcription-test"
		}
		if _, err := m.Register(context.Background(), auth); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(auth.ID, provider, []*registry.ModelInfo{{ID: "speech-test"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	}
	return NewOpenAIAPIHandler(handlers.NewBaseAPIHandlers(&cfg.SDKConfig, m))
}

func TestAudioTranscriptionsUploadAndBase64(t *testing.T) {
	audio := []byte{0, 1, 255, 13, 10, 128, 42}
	for _, input := range []string{"json", "multipart", "multipart-without-filename"} {
		for _, output := range []struct{ format, contentType, body string }{
			{"json", "application/json", `{"text":"Hello world"}`},
			{"text", "text/plain; charset=utf-8", "Hello world\n"},
			{"vtt", "text/vtt", "WEBVTT\n\n00:00.000 --> 00:01.000\nHello world\n"},
		} {
			t.Run(input+"/"+output.format, func(t *testing.T) {
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.URL.Path != "/v1/audio/transcriptions" || r.Method != http.MethodPost {
						t.Errorf("unexpected upstream route: %s %s", r.Method, r.URL.Path)
					}
					if r.Header.Get("Authorization") != "Bearer test-key" {
						t.Error("missing upstream credentials")
					}
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					defer func() {
						if err := r.MultipartForm.RemoveAll(); err != nil {
							t.Error(err)
						}
					}()
					if r.FormValue("model") != "whisper-1" {
						t.Errorf("upstream model = %q", r.FormValue("model"))
					}
					if r.FormValue("language") != "en" || r.FormValue("prompt") != "Names: Ada & Grace" || r.FormValue("response_format") != output.format {
						t.Errorf("lost fields: %v", r.MultipartForm.Value)
					}
					if !reflect.DeepEqual(r.MultipartForm.Value["timestamp_granularities[]"], []string{"word", "segment"}) {
						t.Errorf("lost timestamps: %v", r.MultipartForm.Value)
					}
					if input == "multipart-without-filename" {
						if !bytes.Equal([]byte(r.FormValue("file")), audio) {
							t.Errorf("audio without filename changed: %q", r.FormValue("file"))
						}
						w.Header().Set("Content-Type", output.contentType)
						_, _ = io.WriteString(w, output.body)
						return
					}
					file, header, err := r.FormFile("file")
					if err != nil {
						t.Error(err)
						return
					}
					data, errRead := io.ReadAll(file)
					if errClose := file.Close(); errClose != nil {
						t.Error(errClose)
					}
					if errRead != nil || !bytes.Equal(data, audio) {
						t.Errorf("audio changed: %v, %v", data, errRead)
					}
					if input == "multipart" && (header.Filename != "recording.wav" || header.Header.Get("X-Audio-Metadata") != "preserved") {
						t.Errorf("file metadata changed: %v", header)
					}
					w.Header().Set("Content-Type", output.contentType)
					_, _ = io.WriteString(w, output.body)
				}))
				defer upstream.Close()
				h := transcriptionTestHandler(t, upstream.URL+"/v1", true)
				var body bytes.Buffer
				contentType := "application/json"
				if input == "json" {
					fmt.Fprintf(&body, `{"model":"speech-test","input_audio":{"data":%q,"format":"wav"},"response_format":%q,"language":"en","prompt":"Names: Ada & Grace","timestamp_granularities":["word","segment"]}`, base64.StdEncoding.EncodeToString(audio), output.format)
				} else {
					writer := multipart.NewWriter(&body)
					for _, field := range [][2]string{{"model", "speech-test"}, {"language", "en"}, {"prompt", "Names: Ada & Grace"}, {"response_format", output.format}, {"timestamp_granularities[]", "word"}, {"timestamp_granularities[]", "segment"}} {
						if err := writer.WriteField(field[0], field[1]); err != nil {
							t.Fatal(err)
						}
					}
					disposition := `form-data; name="file"; filename="recording.wav"`
					if input == "multipart-without-filename" {
						disposition = `form-data; name="file"`
					}
					file, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {disposition}, "Content-Type": {"audio/wav"}, "X-Audio-Metadata": {"preserved"}})
					if err != nil {
						t.Fatal(err)
					}
					if _, errWrite := file.Write(audio); errWrite != nil {
						t.Fatal(errWrite)
					}
					if errClose := writer.Close(); errClose != nil {
						t.Fatal(errClose)
					}
					contentType = writer.FormDataContentType()
				}
				response := performImagesEndpointRequest(t, "/v1/audio/transcriptions", contentType, &body, h.AudioTranscriptions)
				if response.Code != 200 || response.Body.String() != output.body || response.Header().Get("Content-Type") != output.contentType {
					t.Fatalf("response = %d %v %s", response.Code, response.Header(), response.Body.String())
				}
				if calls.Load() != 1 {
					t.Fatalf("upstream calls = %d", calls.Load())
				}
			})
		}
	}
}

func TestAudioTranscriptionsReturnsProviderErrorsWithoutRetry(t *testing.T) {
	for _, status := range []int{400, 401, 404, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			body := `{"error":{"message":"transcription unavailable","type":"upstream_error"}}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			h := transcriptionTestHandler(t, upstream.URL, true)
			response := performImagesEndpointRequest(t, "/v1/audio/transcriptions", "application/json", strings.NewReader(`{"model":"speech-test","input_audio":{"data":"AAE=","format":"wav"}}`), h.AudioTranscriptions)
			if response.Code != status || response.Body.String() != body {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if calls.Load() != 1 {
				t.Fatalf("retried transcription: %d calls", calls.Load())
			}
		})
	}
}

func TestAudioTranscriptionsUnsupportedProvider(t *testing.T) {
	h := transcriptionTestHandler(t, "http://unused.invalid", false)
	response := performImagesEndpointRequest(t, "/v1/audio/transcriptions", "application/json", strings.NewReader(`{"model":"speech-test","input_audio":{"data":"AAE=","format":"wav"}}`), h.AudioTranscriptions)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "no provider supports audio transcriptions") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestAudioTranscriptionsInvalidInput(t *testing.T) {
	h := NewOpenAIAPIHandler(handlers.NewBaseAPIHandlers(nil, nil))
	for _, body := range []string{
		`{`, `[]`, `{}`, `{"model":123,"input_audio":{"data":"AAE=","format":"wav"}}`,
		`{"model":"m","input_audio":{"data":"invalid!","format":"wav"}}`,
		`{"model":"m","input_audio":{"data":"AAE="}}`,
		`{"model":"m","input_audio":{"data":"","format":"wav"}}`,
		`{"input_audio":{"data":"AAE=","format":"wav"}}`,
		`{"model":"m","input_audio":{"data":"AAE=","format":"wav"},"stream":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			response := performImagesEndpointRequest(t, "/v1/audio/transcriptions", "application/json", strings.NewReader(body), h.AudioTranscriptions)
			if response.Code != 400 {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
	for _, contentType := range []string{"multipart/form-data", "multipart/form-data; boundary=broken", "text/plain"} {
		response := performImagesEndpointRequest(t, "/v1/audio/transcriptions", contentType, strings.NewReader("broken"), h.AudioTranscriptions)
		if response.Code != 400 {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
	}
}
