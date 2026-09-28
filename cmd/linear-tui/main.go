package main

import (
	"context"
	"fmt"
	"os"

	"github.com/roeyazroel/linear-tui/internal/auth"
	"github.com/roeyazroel/linear-tui/internal/auth/oauth"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
	"github.com/roeyazroel/linear-tui/internal/tui"
	"github.com/roeyazroel/linear-tui/internal/updatecheck"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes the CLI entrypoint and returns a process exit code.
func run(args []string) int {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Println(VersionInfo())
		return 0
	}

	if len(args) > 0 && args[0] == "auth" {
		return runAuth(args[1:])
	}

	return runTUI()
}

// runAuth handles `linear-tui auth ...` subcommands.
func runAuth(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		auth.PrintAuthUsage(os.Stdout)
		return 0
	}

	storePath, err := auth.CredentialsPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving credentials path: %v\n", err)
		return 1
	}

	clientID := auth.ClientID(oauth.DefaultClientID)
	oauthClient := oauth.NewClient(oauth.ClientConfig{ClientID: clientID})
	ctx := context.Background()

	switch args[0] {
	case "login":
		if clientID == "" {
			fmt.Fprintf(os.Stderr, "Error: OAuth client id is not configured. Set %s.\n", config.LinearClientIDEnv)
			return 1
		}
		fmt.Println("Opening browser for Linear authorization...")
		if err := auth.Login(ctx, auth.LoginOptions{
			ClientID:    clientID,
			StorePath:   storePath,
			OAuthClient: oauthClient,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
			return 1
		}
		fmt.Println("Login successful. Credentials stored in", storePath)
		return 0
	case "logout":
		if err := auth.Logout(ctx, auth.LogoutOptions{
			StorePath:   storePath,
			OAuthClient: oauthClient,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Logout failed: %v\n", err)
			return 1
		}
		fmt.Println("Logged out. Stored OAuth credentials removed.")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown auth command %q\n\n", args[0])
		auth.PrintAuthUsage(os.Stderr)
		return 1
	}
}

// runTUI boots the interactive application with resolved credentials.
func runTUI() int {
	settingsPath, err := config.ConfigFilePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error determining settings path: %v\n", err)
		return 1
	}

	settings, err := config.EnsureSettingsFile(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading settings file: %v\n", err)
		return 1
	}

	storePath, err := auth.CredentialsPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving credentials path: %v\n", err)
		return 1
	}

	clientID := auth.ClientID(oauth.DefaultClientID)
	oauthClient := oauth.NewClient(oauth.ClientConfig{ClientID: clientID})
	ctx := context.Background()

	apiKey := os.Getenv(config.LinearAPIKeyEnv)
	resolved, err := auth.Resolve(ctx, apiKey, storePath, oauthClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading authentication: %v\n", err)
		return 1
	}

	cfg, err := config.ConfigFromSettings(resolved.Token, settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		return 1
	}

	logLevel := parseLogLevel(cfg.LogLevel)
	if err := logger.Init(cfg.LogFile, logLevel); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logger: %v\n", err)
		return 1
	}
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing logger: %v\n", err)
		}
	}()

	logger.Info("app.main: application starting")
	logger.Debug("app.main: configuration endpoint=%s page_size=%d cache_ttl=%s auth_source=%s",
		cfg.APIEndpoint, cfg.PageSize, cfg.CacheTTL, resolved.Source)

	clientCfg := linearapi.ClientConfig{
		Token:     cfg.LinearAPIKey,
		UseBearer: resolved.Source == auth.TokenSourceOAuth,
		Endpoint:  cfg.APIEndpoint,
		Timeout:   cfg.Timeout,
	}
	if resolved.Source == auth.TokenSourceOAuth {
		clientCfg.OnUnauthorized = auth.NewRefreshFunc(storePath, oauthClient)
	}

	apiClient := linearapi.NewClient(clientCfg)

	promptTemplates := config.DefaultAgentPromptTemplates()
	promptsPath, err := config.PromptTemplatesFilePath()
	if err != nil {
		logger.Warning("app.main: failed to resolve prompts file path: %v", err)
	} else {
		templates, err := config.EnsurePromptTemplatesFile(promptsPath)
		if err != nil {
			logger.Warning("app.main: failed to load prompts file path=%s error=%v", promptsPath, err)
		} else {
			promptTemplates = templates
		}
	}

	app := tui.NewApp(apiClient, cfg, promptTemplates)
	app.SetStartupUpdateCheck(func(ctx context.Context) string {
		return updatecheck.Check(ctx, Version)
	})

	if err := app.Run(); err != nil {
		logger.ErrorWithErr(err, "app.main: application error")
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		if closeErr := logger.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Error closing logger: %v\n", closeErr)
		}
		return 1
	}

	logger.Info("app.main: application shutdown")
	return 0
}

// parseLogLevel converts a string log level to a logger.LogLevel.
func parseLogLevel(level string) logger.LogLevel {
	switch level {
	case "debug":
		return logger.LevelDebug
	case "info":
		return logger.LevelInfo
	case "warning":
		return logger.LevelWarning
	case "error":
		return logger.LevelError
	default:
		return logger.LevelWarning
	}
}
