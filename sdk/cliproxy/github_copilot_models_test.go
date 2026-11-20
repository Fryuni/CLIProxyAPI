package cliproxy

import (
	"errors"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func copilotCatalogTestAuth(token string) *coreauth.Auth {
	return &coreauth.Auth{ID: "copilot.json", Provider: copilot.Provider, Metadata: map[string]any{"access_token": token}}
}

func TestCopilotCatalogsRefetchWhenTokenChanges(t *testing.T) {
	var catalogs copilotCatalogs
	now := time.Unix(1_000_000, 0)
	auth := copilotCatalogTestAuth("gho_one")
	models := []copilot.Model{{ID: "gpt-6-luna", Endpoints: []string{copilot.EndpointResponses}}}

	if _, ok, due := catalogs.cachedModels(auth, now); ok || !due {
		t.Fatalf("empty cache: ok=%v due=%v, want fetch", ok, due)
	}
	key, started := catalogs.begin(auth)
	if !started {
		t.Fatal("first fetch did not start")
	}
	if _, again := catalogs.begin(auth); again {
		t.Fatal("concurrent fetch for the same inputs started twice")
	}
	if !catalogs.finish(auth.ID, key, models, nil, now) {
		t.Fatal("current fetch was not recorded")
	}
	if got, ok, due := catalogs.cachedModels(auth, now.Add(time.Minute)); !ok || due || len(got) != 1 {
		t.Fatalf("fresh cache: models=%v ok=%v due=%v", got, ok, due)
	}
	if _, _, due := catalogs.cachedModels(auth, now.Add(registry.ModelsRefreshInterval)); !due {
		t.Fatal("stale catalog is not due for refresh")
	}

	rotated := copilotCatalogTestAuth("gho_two")
	if _, ok, due := catalogs.cachedModels(rotated, now); ok || !due {
		t.Fatalf("rotated token served the previous account's catalog: ok=%v due=%v", ok, due)
	}
	if _, startedRotated := catalogs.begin(rotated); !startedRotated {
		t.Fatal("rotated token fetch did not start")
	}
	// The fetch for the old token finishing late must not publish.
	if catalogs.finish(auth.ID, key, models, nil, now) {
		t.Fatal("superseded fetch result was accepted")
	}
}

func TestCopilotCatalogsRetryFailedFetchAfterInterval(t *testing.T) {
	var catalogs copilotCatalogs
	now := time.Unix(1_000_000, 0)
	auth := copilotCatalogTestAuth("gho_one")
	key, _ := catalogs.begin(auth)
	catalogs.finish(auth.ID, key, nil, errors.New("boom"), now)
	if _, _, due := catalogs.cachedModels(auth, now.Add(time.Minute)); due {
		t.Fatal("failed fetch retried before the retry interval")
	}
	if _, _, due := catalogs.cachedModels(auth, now.Add(copilotCatalogRetryInterval)); !due {
		t.Fatal("failed fetch not retried after the retry interval")
	}
}
