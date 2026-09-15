package registry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type devinModelsTransport func(*http.Request) (*http.Response, error)

func (f devinModelsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type devinModelsBody struct {
	io.Reader
	ctx    context.Context
	closed bool
}

func (b *devinModelsBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.Reader.Read(p)
}

func (b *devinModelsBody) Close() error {
	b.closed = true
	return nil
}

func TestFetchDevinModelsKeepsBodyContextAlive(t *testing.T) {
	const payload = `{"devin":[{"id":"devin/streamed"}]}`
	originalTransport, originalURLs := http.DefaultTransport, devinModelsURLs
	t.Cleanup(func() {
		http.DefaultTransport, devinModelsURLs = originalTransport, originalURLs
	})
	devinModelsURLs = []string{"https://catalog.test/devin_models.json"}
	var body *devinModelsBody
	http.DefaultTransport = devinModelsTransport(func(req *http.Request) (*http.Response, error) {
		body = &devinModelsBody{Reader: strings.NewReader(payload), ctx: req.Context()}
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})
	data, source := fetchDevinModelsFromRemote(context.Background())
	if string(data) != payload || source != devinModelsURLs[0] {
		t.Fatalf("fetch = %q from %q; want complete response body", data, source)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
	if _, deadline := body.ctx.Deadline(); deadline {
		t.Fatal("catalog request introduced a deadline beyond credential acquisition")
	}
}

func TestDevinModelRefreshNotifiesRegistrations(t *testing.T) {
	originalStore, originalURLs := devinCatalogStore, devinModelsURLs
	devinCatalogStore = &devinModelsStore{}
	refreshCallbackMu.Lock()
	originalCallback, originalPending := refreshCallback, pendingRefreshChanges
	refreshCallback, pendingRefreshChanges = nil, nil
	refreshCallbackMu.Unlock()
	t.Cleanup(func() {
		devinCatalogStore, devinModelsURLs = originalStore, originalURLs
		refreshCallbackMu.Lock()
		refreshCallback, pendingRefreshChanges = originalCallback, originalPending
		refreshCallbackMu.Unlock()
	})
	data := `{"devin":[{"id":"devin/first"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, data)
	}))
	defer server.Close()
	devinModelsURLs = []string{server.URL}
	// Startup changes must also be delivered if registration happens later.
	tryRefreshDevinModels(context.Background(), "startup test")
	var notifications [][]string
	SetModelRefreshCallback(func(providers []string) {
		notifications = append(notifications, append([]string(nil), providers...))
		if models := GetDevinModels(); len(models) != 1 {
			t.Errorf("refresh callback saw %d models", len(models))
		}
	})
	if !reflect.DeepEqual(notifications, [][]string{{"devin"}}) {
		t.Fatalf("startup notifications = %v", notifications)
	}
	tryRefreshDevinModels(context.Background(), "unchanged test")
	if len(notifications) != 1 {
		t.Fatalf("unchanged catalog triggered callback: %v", notifications)
	}
	data = `{"devin":[{"id":"devin/replacement"}]}`
	tryRefreshDevinModels(context.Background(), "changed test")
	if !reflect.DeepEqual(notifications, [][]string{{"devin"}, {"devin"}}) {
		t.Fatalf("changed catalog notifications = %v", notifications)
	}
	if models := GetDevinModels(); len(models) != 1 || models[0].ID != "devin/replacement" {
		t.Fatalf("catalog = %v", models)
	}
	data = `invalid JSON`
	tryRefreshDevinModels(context.Background(), "rejected test")
	if len(notifications) != 2 {
		t.Fatalf("invalid catalog triggered callback: %v", notifications)
	}
}
