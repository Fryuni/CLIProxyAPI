package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const claudeQuotaSource = "builtin:claude:"
const claudeQuotaBaseURL = "https://api.anthropic.com/api/oauth/"

// claudeQuotaHeaders matches the built-in management card, independently of
// executor device defaults. Matching and probing must use the same representation.
func claudeQuotaHeaders(resource, token string) http.Header {
	headers := http.Header{
		"Authorization":  {"Bearer " + token},
		"Content-Type":   {"application/json"},
		"Anthropic-Beta": {"oauth-2025-04-20"},
		"User-Agent":     {"claude-cli/2.1.280 (external, cli)"},
	}
	if resource == "usage" {
		headers.Set("Accept", "application/json")
	}
	return headers
}

func isClaudeQuotaCredential(auth *coreauth.Auth) bool {
	return auth != nil && strings.EqualFold(strings.TrimSpace(auth.Provider), "claude") &&
		strings.TrimSpace(auth.Attributes["api_key"]) == "" && tokenValueFromMetadata(auth.Metadata) != ""
}

func claudeQuotaKey(authID, resource string) quotaCacheKey {
	return quotaCacheKey{pluginID: claudeQuotaSource + resource, authID: authID}
}

func claudeQuotaResource(key quotaCacheKey) (string, bool) {
	switch key.pluginID {
	case claudeQuotaSource + "usage":
		return "usage", true
	case claudeQuotaSource + "profile":
		return "profile", true
	default:
		return "", false
	}
}

// serveClaudeQuotaAPICall bridges the built-in management card's raw APICall
// contract to the quota cache. Other requests retain generic APICall behavior.
func (h *Handler) serveClaudeQuotaAPICall(c *gin.Context, body apiCallRequest, auth *coreauth.Auth) bool {
	if !c.GetBool(ConfigV8ContextKey) || !isClaudeQuotaCredential(auth) ||
		!strings.EqualFold(strings.TrimSpace(body.Method), http.MethodGet) || body.Data != "" || strings.TrimSpace(body.ProxyURL) != "" {
		return false
	}
	resource := strings.TrimPrefix(strings.TrimSpace(body.URL), claudeQuotaBaseURL)
	if resource != "usage" && resource != "profile" {
		return false
	}
	// Never serve this credential's cached response for caller-supplied credentials,
	// host overrides, or headers that may select a different upstream representation.
	canonical := claudeQuotaHeaders(resource, "$TOKEN$")
	seen := make(map[string]bool, len(body.Header))
	for key, value := range body.Header {
		name := http.CanonicalHeaderKey(key)
		if seen[name] || canonical.Get(name) == "" || value != canonical.Get(name) {
			return false
		}
		seen[name] = true
	}
	if len(seen) != len(canonical) {
		return false
	}
	key := claudeQuotaKey(auth.ID, resource)
	if entry, ok := h.quotas.get(key, credentialStampOf(auth)); ok {
		c.Header(quotaFetchedAtHeader, entry.fetchedAt.Format(time.RFC3339))
		c.JSON(http.StatusOK, entry.apiResp)
		return true
	}
	entry, _, errFetch := waitQuotaFlight(c.Request.Context(), h.startClaudeQuotaFetch(auth, resource, false))
	if errFetch != nil {
		// With no successful observation, preserve the upstream APICall contract;
		// the completed failure is shared until the next background retry is due.
		if entry.apiResp.StatusCode != 0 {
			c.JSON(http.StatusOK, entry.apiResp)
		} else {
			c.JSON(http.StatusBadGateway, gin.H{"error": "quota is not available yet; background refresh will retry"})
		}
		return true
	}
	c.Header(quotaFetchedAtHeader, entry.fetchedAt.Format(time.RFC3339))
	c.JSON(http.StatusOK, entry.apiResp)
	return true
}

func (h *Handler) startClaudeQuotaFetch(auth *coreauth.Auth, resource string, refresh bool) *quotaFlight {
	return h.startQuotaFetch(claudeQuotaKey(auth.ID, resource), auth, quotaRefreshInterval, refresh, func(ctx context.Context) (quotaCacheEntry, bool, error) {
		req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, claudeQuotaBaseURL+resource, nil)
		if errRequest != nil {
			return quotaCacheEntry{}, true, errRequest
		}
		req.Header = claudeQuotaHeaders(resource, tokenValueFromMetadata(auth.Metadata))
		h.mu.Lock()
		transport := h.apiCallTransport(auth, "")
		h.mu.Unlock()
		client := &http.Client{Transport: transport}
		resp, errDo := client.Do(req)
		if errDo != nil {
			return quotaCacheEntry{}, true, errDo
		}
		defer func() {
			if errClose := resp.Body.Close(); errClose != nil {
				log.WithError(errClose).Debug("Claude quota response body close failed")
			}
		}()
		raw, errRead := io.ReadAll(resp.Body)
		if errRead != nil {
			return quotaCacheEntry{}, true, errRead
		}
		entry := quotaCacheEntry{apiResp: apiCallResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: string(raw)}}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return entry, true, fmt.Errorf("Claude %s quota probe returned HTTP %d", resource, resp.StatusCode)
		}
		var payload map[string]json.RawMessage
		if errJSON := json.Unmarshal(raw, &payload); errJSON != nil || len(payload) == 0 || payload["error"] != nil {
			return entry, true, fmt.Errorf("Claude %s quota probe returned an invalid response", resource)
		}
		return entry, true, nil
	})
}
