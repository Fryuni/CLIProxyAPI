package executor

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestTranslateCodexRequestPairReusesEqualPayload(t *testing.T) {
	from := sdktranslator.Format("codex-test-from-equal")
	to := sdktranslator.Format("codex-test-to-equal")
	var calls int32
	sdktranslator.Register(from, to, func(model string, rawJSON []byte, stream bool) []byte {
		atomic.AddInt32(&calls, 1)
		if model != "test-model" {
			t.Errorf("model = %q, want test-model", model)
		}
		if !stream {
			t.Error("stream = false, want true")
		}
		return append([]byte(nil), rawJSON...)
	}, sdktranslator.ResponseTransform{})

	payload := []byte(`{"model":"test-model","input":[{"role":"user"}]}`)
	originalTranslated, body := translateCodexRequestPair(context.Background(), from, to, "test-model", payload, bytes.Clone(payload), true)

	if gotCalls := atomic.LoadInt32(&calls); gotCalls != 1 {
		t.Fatalf("TranslateRequest calls = %d, want 1", gotCalls)
	}
	if !bytes.Equal(originalTranslated, body) {
		t.Fatalf("translated payloads differ: original=%s body=%s", originalTranslated, body)
	}
}

func TestTranslateCodexRequestPairTranslatesDifferentPayloads(t *testing.T) {
	from := sdktranslator.Format("codex-test-from-different")
	to := sdktranslator.Format("codex-test-to-different")
	var calls int32
	sdktranslator.Register(from, to, func(_ string, rawJSON []byte, _ bool) []byte {
		atomic.AddInt32(&calls, 1)
		return append([]byte(nil), rawJSON...)
	}, sdktranslator.ResponseTransform{})

	originalPayload := []byte(`{"model":"test-model","input":[{"role":"system"}]}`)
	payload := []byte(`{"model":"test-model","input":[{"role":"user"}]}`)
	originalTranslated, body := translateCodexRequestPair(context.Background(), from, to, "test-model", originalPayload, payload, false)

	if gotCalls := atomic.LoadInt32(&calls); gotCalls != 2 {
		t.Fatalf("TranslateRequest calls = %d, want 2", gotCalls)
	}
	if !bytes.Equal(originalTranslated, originalPayload) {
		t.Fatalf("original translated = %s, want %s", originalTranslated, originalPayload)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("body = %s, want %s", body, payload)
	}
}

type cancellationObservingPluginHooks struct {
	started  chan struct{}
	finished chan struct{}
}

func (h *cancellationObservingPluginHooks) NormalizeRequest(ctx context.Context, _, _ sdktranslator.Format, _ string, body []byte, _ bool) []byte {
	close(h.started)
	<-ctx.Done()
	close(h.finished)
	return body
}

func (*cancellationObservingPluginHooks) TranslateRequest(context.Context, sdktranslator.Format, sdktranslator.Format, string, []byte, bool) ([]byte, bool) {
	return nil, false
}

func (*cancellationObservingPluginHooks) NormalizeResponseBefore(context.Context, sdktranslator.Format, sdktranslator.Format, string, []byte, []byte, []byte, bool) []byte {
	return nil
}

func (*cancellationObservingPluginHooks) TranslateResponse(context.Context, sdktranslator.Format, sdktranslator.Format, string, []byte, []byte, []byte, bool) ([]byte, bool) {
	return nil, false
}

func (*cancellationObservingPluginHooks) NormalizeResponseAfter(context.Context, sdktranslator.Format, sdktranslator.Format, string, []byte, []byte, []byte, bool) []byte {
	return nil
}

func TestTranslateCodexRequestPairCancelsCompatRequestNormalizer(t *testing.T) {
	hooks := &cancellationObservingPluginHooks{
		started:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	sdktranslator.SetPluginHooks(hooks)
	t.Cleanup(func() { sdktranslator.SetPluginHooks(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		payload := []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hello"}]}`)
		translateCodexRequestPair(ctx, sdktranslator.FormatClaude, sdktranslator.FormatCodex, "claude-test", payload, payload, false, true)
	}()

	select {
	case <-hooks.started:
	case <-time.After(time.Second):
		t.Fatal("request normalizer did not start")
	}
	cancel()

	select {
	case <-hooks.finished:
	case <-time.After(time.Second):
		t.Fatal("request normalizer did not observe executor request cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("compat request translation did not return after cancellation")
	}
}
