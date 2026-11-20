package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// GitHubCopilotTokenMetadataKey carries an existing GitHub OAuth token (for
// example from `gh auth token`) in LoginOptions.Metadata. When set, Login
// validates and stores it instead of running the device flow.
const GitHubCopilotTokenMetadataKey = "github_token"

// GitHubCopilotAuthenticator logs in to GitHub Copilot with a GitHub OAuth token.
type GitHubCopilotAuthenticator struct{}

// NewGitHubCopilotAuthenticator constructs the GitHub Copilot authenticator.
func NewGitHubCopilotAuthenticator() Authenticator { return &GitHubCopilotAuthenticator{} }

// Provider returns the provider key.
func (GitHubCopilotAuthenticator) Provider() string { return copilot.Provider }

// RefreshLead disables scheduled refresh; GitHub OAuth tokens do not expire.
func (GitHubCopilotAuthenticator) RefreshLead() *time.Duration { return nil }

// Login runs the GitHub device flow, or imports a supplied token, and returns
// a credential for an account with Copilot chat access.
func (a GitHubCopilotAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	svc := copilot.NewAuth(cfg, opts.ProxyURL)

	token := strings.TrimSpace(opts.Metadata[GitHubCopilotTokenMetadataKey])
	if token == "" {
		var err error
		if token, err = deviceLoginGitHubCopilot(ctx, svc, opts); err != nil {
			return nil, err
		}
	}
	account, err := svc.LookupAccount(ctx, token)
	if err != nil {
		return nil, err
	}

	storage := &copilot.TokenStorage{
		AccessToken: token,
		Login:       account.Login,
		Name:        account.Name,
		Email:       account.Email,
		Plan:        account.Plan,
		BaseURL:     account.BaseURL,
	}
	fileName := copilot.CredentialFileName(account.Login, token)
	label := account.Login
	if label == "" {
		label = "GitHub Copilot"
	}
	metadata := storage.Metadata()
	if opts.ProxyURL != "" {
		metadata["proxy_url"] = opts.ProxyURL
	}
	attrs := map[string]string{"auth_kind": "oauth"}
	if account.BaseURL != "" {
		attrs["base_url"] = account.BaseURL
	}
	return &coreauth.Auth{
		ID:         fileName,
		Provider:   a.Provider(),
		FileName:   fileName,
		Label:      label,
		ProxyURL:   opts.ProxyURL,
		Storage:    storage,
		Metadata:   metadata,
		Attributes: attrs,
	}, nil
}

func deviceLoginGitHubCopilot(ctx context.Context, svc *copilot.Auth, opts *LoginOptions) (string, error) {
	fmt.Println("Starting GitHub Copilot authentication...")
	code, err := svc.StartDeviceFlow(ctx)
	if err != nil {
		return "", err
	}
	fmt.Printf("\nTo authenticate, visit %s and enter code: %s\n\n", code.VerificationURI, code.UserCode)
	if !opts.NoBrowser {
		if browser.IsAvailable() {
			if errOpen := browser.OpenURL(code.VerificationURI); errOpen != nil {
				log.Warnf("Failed to open browser automatically: %v", errOpen)
			}
		} else {
			log.Warn("No browser available; please open the URL manually")
		}
	}
	fmt.Println("Waiting for authorization...")
	return svc.WaitForToken(ctx, code)
}
