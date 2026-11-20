package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	// ClientID is the GitHub Copilot CLI OAuth application. Its tokens reach
	// every model the CLI integration serves.
	ClientID = "Ov23ctDVkRmgkPke0Mmm"
	// OAuthScope is the narrowest scope that still yields a Copilot-entitled token.
	OAuthScope = "read:user"

	deviceCodeURL   = "https://github.com/login/device/code"
	accessTokenURL  = "https://github.com/login/oauth/access_token"
	userURL         = "https://api.github.com/user"
	copilotUserURL  = "https://api.github.com/copilot_internal/user"
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	defaultPollInterval = 5 * time.Second
	maxPollDuration     = 15 * time.Minute
	// httpClientTimeout bounds credential-acquisition HTTP calls only.
	httpClientTimeout = 30 * time.Second
	maxResponseBytes  = 4 << 20
)

// DeviceCode is GitHub's device authorization response.
type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresIn       int
	Interval        int
}

// Account describes the GitHub account behind a token and its Copilot entitlement.
type Account struct {
	Login   string
	Name    string
	Email   string
	Plan    string
	BaseURL string
}

// Auth performs GitHub device login and Copilot account discovery.
type Auth struct {
	httpClient *http.Client
}

// NewAuth builds an Auth that honors the configured or explicit login proxy.
func NewAuth(cfg *config.Config, proxyURL string) *Auth {
	var sdkCfg config.SDKConfig
	if cfg != nil {
		sdkCfg = cfg.SDKConfig
	}
	if strings.TrimSpace(proxyURL) != "" {
		sdkCfg.ProxyURL = strings.TrimSpace(proxyURL)
	}
	return &Auth{httpClient: util.SetProxy(&sdkCfg, &http.Client{Timeout: httpClientTimeout})}
}

// StartDeviceFlow requests a device code for the Copilot CLI OAuth app.
func (a *Auth) StartDeviceFlow(ctx context.Context) (*DeviceCode, error) {
	form := url.Values{"client_id": {ClientID}, "scope": {OAuthScope}}
	body, status, err := a.postForm(ctx, deviceCodeURL, form)
	if err != nil {
		return nil, fmt.Errorf("copilot auth: request device code: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("copilot auth: request device code: HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	code := &DeviceCode{
		DeviceCode:      ojson.GetBytesNoCopy(body, "device_code").String(),
		UserCode:        ojson.GetBytesNoCopy(body, "user_code").String(),
		VerificationURI: ojson.GetBytesNoCopy(body, "verification_uri").String(),
		ExpiresIn:       int(ojson.GetBytesNoCopy(body, "expires_in").Int()),
		Interval:        int(ojson.GetBytesNoCopy(body, "interval").Int()),
	}
	if code.DeviceCode == "" || code.UserCode == "" {
		return nil, fmt.Errorf("copilot auth: device code response missing fields")
	}
	return code, nil
}

// WaitForToken polls until the user authorizes the device code and returns the OAuth token.
func (a *Auth) WaitForToken(ctx context.Context, code *DeviceCode) (string, error) {
	if code == nil {
		return "", fmt.Errorf("copilot auth: device code is nil")
	}
	interval := defaultPollInterval
	if code.Interval > 0 {
		interval = time.Duration(code.Interval) * time.Second
	}
	wait := maxPollDuration
	if code.ExpiresIn > 0 && time.Duration(code.ExpiresIn)*time.Second < wait {
		wait = time.Duration(code.ExpiresIn) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	form := url.Values{"client_id": {ClientID}, "device_code": {code.DeviceCode}, "grant_type": {deviceGrantType}}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("copilot auth: authorization timed out or canceled: %w", ctx.Err())
		case <-timer.C:
		}
		body, _, err := a.postForm(ctx, accessTokenURL, form)
		if err != nil {
			log.Warnf("copilot auth: poll token: %v (retrying)", err)
			timer.Reset(interval)
			continue
		}
		if token := ojson.GetBytesNoCopy(body, "access_token").String(); token != "" {
			return token, nil
		}
		switch errCode := ojson.GetBytesNoCopy(body, "error").String(); errCode {
		case "authorization_pending":
		case "slow_down":
			if next := ojson.GetBytesNoCopy(body, "interval").Int(); next > 0 {
				interval = time.Duration(next) * time.Second
			} else {
				interval += 5 * time.Second
			}
		case "access_denied":
			return "", fmt.Errorf("copilot auth: access was denied")
		case "expired_token":
			return "", fmt.Errorf("copilot auth: device code expired")
		default:
			return "", fmt.Errorf("copilot auth: token error %q: %s", errCode, ojson.GetBytesNoCopy(body, "error_description").String())
		}
		timer.Reset(interval)
	}
}

// LookupAccount resolves the GitHub identity and Copilot entitlement of a token.
// It fails when GitHub reports that the account cannot use Copilot chat.
func (a *Auth) LookupAccount(ctx context.Context, token string) (*Account, error) {
	userBody, status, err := a.get(ctx, userURL, token)
	if err != nil {
		return nil, fmt.Errorf("copilot auth: fetch GitHub user: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("copilot auth: fetch GitHub user: HTTP %d: %s", status, strings.TrimSpace(string(userBody)))
	}
	account := &Account{
		Login: ojson.GetBytesNoCopy(userBody, "login").String(),
		Name:  ojson.GetBytesNoCopy(userBody, "name").String(),
		Email: ojson.GetBytesNoCopy(userBody, "email").String(),
	}
	copilotBody, status, err := a.get(ctx, copilotUserURL, token)
	if err != nil {
		return nil, fmt.Errorf("copilot auth: fetch Copilot entitlement: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("copilot auth: fetch Copilot entitlement: HTTP %d: %s", status, strings.TrimSpace(string(copilotBody)))
	}
	if chat := ojson.GetBytesNoCopy(copilotBody, "chat_enabled"); chat.Exists() && !chat.Bool() {
		return nil, fmt.Errorf("copilot auth: GitHub account %s has no Copilot chat access", account.Login)
	}
	account.Plan = ojson.GetBytesNoCopy(copilotBody, "copilot_plan").String()
	account.BaseURL = strings.TrimRight(strings.TrimSpace(ojson.GetBytesNoCopy(copilotBody, "endpoints.api").String()), "/")
	return account, nil
}

func (a *Auth) postForm(ctx context.Context, target string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return a.do(req)
}

func (a *Auth) get(ctx context.Context, target, token string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return a.do(req)
}

func (a *Auth) do(req *http.Request) ([]byte, int, error) {
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("copilot auth: close response body: %v", errClose)
		}
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// TokenStorage is the persisted form of a Copilot credential.
type TokenStorage struct {
	AccessToken string
	Login       string
	Name        string
	Email       string
	Plan        string
	BaseURL     string

	metadata map[string]any
}

// SetMetadata merges token-store status fields into the saved file.
func (ts *TokenStorage) SetMetadata(meta map[string]any) { ts.metadata = meta }

// Metadata returns the auth-file fields for this credential.
func (ts *TokenStorage) Metadata() map[string]any {
	data := map[string]any{
		"type":         Provider,
		"auth_kind":    "oauth",
		"access_token": ts.AccessToken,
	}
	for key, value := range map[string]string{
		"login":    ts.Login,
		"name":     ts.Name,
		"email":    ts.Email,
		"plan":     ts.Plan,
		"base_url": ts.BaseURL,
	} {
		if value != "" {
			data[key] = value
		}
	}
	return data
}

// SaveTokenToFile writes the credential as a JSON auth file.
func (ts *TokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	data := ts.Metadata()
	for key, value := range ts.metadata {
		if _, owned := data[key]; !owned {
			data[key] = value
		}
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	doc := ojson.NewDoc([]byte(`{}`))
	for _, key := range keys {
		if errSet := doc.Set(ojson.Escape(key), data[key]); errSet != nil {
			return fmt.Errorf("copilot token storage: encode %s: %w", key, errSet)
		}
	}
	if errMkdir := os.MkdirAll(filepath.Dir(authFilePath), 0o700); errMkdir != nil {
		return fmt.Errorf("copilot token storage: create directory: %w", errMkdir)
	}
	if errWrite := os.WriteFile(authFilePath, doc.Bytes(), 0o600); errWrite != nil {
		return fmt.Errorf("copilot token storage: write file: %w", errWrite)
	}
	return nil
}

var unsafeFileChars = regexp.MustCompile(`[^a-z0-9._-]+`)

// CredentialFileName names the auth file for a GitHub login, falling back to
// a token digest when the login is unknown.
func CredentialFileName(login, token string) string {
	clean := unsafeFileChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(login)), "-")
	clean = strings.Trim(clean, "-.")
	if clean != "" {
		return fmt.Sprintf("%s-%s.json", Provider, clean)
	}
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%s-%s.json", Provider, hex.EncodeToString(digest[:6]))
}
