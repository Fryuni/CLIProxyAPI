package handlers

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

type countedRequestBody struct {
	reader io.Reader
	read   int
}

func (r *countedRequestBody) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}
func (*countedRequestBody) Close() error { return nil }

func TestReadRequestBodyWithLimit(t *testing.T) {
	writer, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	oversized := bytes.Repeat([]byte("a"), 4096)
	compressed := writer.EncodeAll(oversized, nil)
	for _, tc := range []struct {
		name        string
		body        []byte
		encoding    string
		knownLength bool
		wantError   bool
	}{
		{"known_oversized", oversized, "", true, true},
		{"chunked_oversized", oversized, "", false, true},
		{"at_limit", oversized[:64], "", true, false},
		{"decoded_oversized", compressed, "zstd", true, true},
		{"stacked_decoded_oversized", writer.EncodeAll(compressed, nil), "zstd,zstd", false, true},
		{"compressed_valid", writer.EncodeAll([]byte(`{"model":"speech"}`), nil), "zstd", true, false},
		{"mislabeled_json", []byte(`{"model":"speech"}`), "zstd", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
			body := &countedRequestBody{reader: bytes.NewReader(tc.body)}
			c.Request.Body = body
			c.Request.ContentLength = -1
			if tc.knownLength {
				c.Request.ContentLength = int64(len(tc.body))
			}
			c.Request.Header.Set("Content-Encoding", tc.encoding)
			got, err := ReadRequestBodyWithLimit(c, 64)
			var limitErr *http.MaxBytesError
			if tc.wantError {
				if !errors.As(err, &limitErr) || got != nil {
					t.Fatalf("body=%q err=%v", got, err)
				}
			} else if err != nil || len(got) > 64 {
				t.Fatalf("body=%q err=%v", got, err)
			}
			if body.read > 65 {
				t.Fatalf("read %d bytes despite limit", body.read)
			}
			if tc.knownLength && len(tc.body) > 64 && body.read != 0 {
				t.Fatal("read known oversized body")
			}
		})
	}
}

func TestReadRequestBodyStillAcceptsUnboundedRequests(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("body"))
	got, err := ReadRequestBody(c)
	if err != nil || string(got) != "body" {
		t.Fatalf("body=%q err=%v", got, err)
	}
}
