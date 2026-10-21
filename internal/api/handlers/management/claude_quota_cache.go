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
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const claudeQuotaSource = "builtin:claude:"
const claudeQuotaBaseURL = "https://api.anthropic.com/api/oauth/"

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
	authorized := false
	for key, value := range body.Header {
		switch strings.ToLower(key) {
		case "authorization":
			if value != "Bearer $TOKEN$" {
				return false
			}
			authorized = true
		case "accept", "content-type":
			if value != "application/json" {
				return false
			}
		case "anthropic-beta":
			if value != "oauth-2025-04-20" {
				return false
			}
		case "user-agent":
		default:
			return false
		}
	}
	if !authorized {
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
		req.Header.Set("Authorization", "Bearer "+tokenValueFromMetadata(auth.Metadata))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		h.mu.Lock()
		req.Header.Set("User-Agent", "claude-cli/"+helps.DefaultClaudeVersion(h.cfg)+" (external, cli)")
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
