package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// githubCopilotTokenEnv names the variable the GitHub Copilot CLI reads its
// token from; when set, login imports it instead of running the device flow,
// e.g. COPILOT_GITHUB_TOKEN="$(gh auth token)".
const githubCopilotTokenEnv = "COPILOT_GITHUB_TOKEN"

// DoGitHubCopilotLogin stores a GitHub Copilot credential from the device flow
// or from COPILOT_GITHUB_TOKEN.
func DoGitHubCopilotLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}
	metadata := map[string]string{}
	if token := strings.TrimSpace(os.Getenv(githubCopilotTokenEnv)); token != "" {
		fmt.Printf("Importing the GitHub token from %s\n", githubCopilotTokenEnv)
		metadata[sdkAuth.GitHubCopilotTokenMetadataKey] = token
	}
	authOpts := &sdkAuth.LoginOptions{
		NoBrowser:    options.NoBrowser,
		CallbackPort: options.CallbackPort,
		ProxyURL:     options.ProxyURL,
		Metadata:     metadata,
		Prompt:       options.Prompt,
	}
	record, savedPath, err := newAuthManager().Login(context.Background(), "github-copilot", cfg, authOpts)
	if err != nil {
		log.Errorf("GitHub Copilot authentication failed: %v", err)
		return
	}
	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}
	if record != nil && record.Label != "" {
		fmt.Printf("Authenticated as %s\n", record.Label)
	}
	fmt.Println("GitHub Copilot authentication successful!")
}
