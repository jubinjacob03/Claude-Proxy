package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"claude-proxy/internal/ansi"
	"claude-proxy/internal/bridge"
	"claude-proxy/internal/licensing"
	"claude-proxy/internal/logx"
)

var appVersion = "dev"

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "serve", "run":
		runServe(args)
	case "claude", "cc":
		runClaude(args)
	case "env", "activate":
		runEnv()
	case "status":
		runStatus()
	case "version", "-version", "--version", "-v":
		fmt.Println("claude-proxy", appVersion)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func mustConfig() *bridge.Config {
	cfg, err := bridge.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	logx.SetLevel(cfg.LogLevel)
	logx.SetFormat(cfg.LogFormat)
	ansi.SetEnabled(cfg.LogFormat != "json")
	return cfg
}

func activateOrExit(cfg *bridge.Config) {
	if cfg.UpstreamAPIKey != "" {
		return
	}

	dir, err := executableDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not determine the install directory:", err)
		os.Exit(1)
	}

	client := licensing.NewClient(cfg.RelayBaseURL)
	token, err := licensing.EnsureActivated(client, dir, cfg.LicenseKey)
	switch {
	case errors.Is(err, licensing.ErrNotLicensed):
		fmt.Fprintln(os.Stderr, "This installation is not licensed.")
		fmt.Fprintln(os.Stderr, "Run the installer again, or set LICENSE_KEY in .env and restart.")
		os.Exit(1)
	case err != nil:
		fmt.Fprintln(os.Stderr, "Licence activation failed:", err)
		os.Exit(1)
	}

	cfg.UpstreamBaseURL = cfg.RelayBaseURL
	cfg.UpstreamAPIKey = token
	cfg.RelayMode = true
	cfg.LicenseKey = ""
}

func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 0, "override PORT")
	host := fs.String("host", "", "override HOST")
	authToken := fs.String("auth-token", "", "override AUTH_TOKEN")
	verbose := fs.Bool("verbose", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	cfg := mustConfig()
	if *port != 0 {
		cfg.Port = *port
	}
	if *host != "" {
		cfg.Host = *host
	}
	if *authToken != "" {
		cfg.AuthToken = *authToken
	}
	if *verbose {
		cfg.LogLevel = logx.LevelDebug
		logx.SetLevel(logx.LevelDebug)
	}
	activateOrExit(cfg)

	logx.Info("claude-proxy %s starting", appVersion)
	if err := bridge.NewServer(cfg, appVersion).Run(); err != nil {
		logx.Error("server exited: %v", err)
		os.Exit(1)
	}
}

func runClaude(extra []string) {
	cfg := mustConfig()
	activateOrExit(cfg)
	srv := bridge.NewServer(cfg, appVersion)
	httpSrv, err := srv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not start proxy on %s: %v\n", cfg.Addr(), err)
		os.Exit(1)
	}

	bin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "claude CLI not found on PATH. Install it with:")
		fmt.Fprintln(os.Stderr, "  npm install -g @anthropic-ai/claude-code")
		shutdown(httpSrv)
		os.Exit(1)
	}

	c := exec.Command(bin, extra...)
	c.Env = claudeEnv(cfg)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	logx.Info("launching claude through http://%s (model %s)", cfg.Addr(), cfg.DefaultModel)

	runErr := c.Run()
	shutdown(httpSrv)
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "claude failed:", runErr)
		os.Exit(1)
	}
}

func claudeEnv(cfg *bridge.Config) []string {
	base := "http://" + cfg.Addr()
	token := cfg.AuthToken
	if token == "" {
		token = "claude-proxy"
	}
	env := append(os.Environ(),
		"ANTHROPIC_BASE_URL="+base,
		"ANTHROPIC_AUTH_TOKEN="+token,
		"ANTHROPIC_API_KEY="+token,
	)
	if cfg.DefaultModel != "" {
		env = append(env,
			"ANTHROPIC_MODEL="+cfg.DefaultModel,
			"ANTHROPIC_DEFAULT_OPUS_MODEL="+cfg.DefaultModel,
			"ANTHROPIC_DEFAULT_SONNET_MODEL="+cfg.DefaultModel,
			"ANTHROPIC_DEFAULT_HAIKU_MODEL="+cfg.DefaultModel,
		)
	}
	return env
}

func runActivate() {
	cfg := mustConfig()
	activateOrExit(cfg)
	fmt.Println("licence activation succeeded")
}

func runEnv() {
	cfg := mustConfig()
	base := "http://" + cfg.Addr()
	token := cfg.AuthToken
	if token == "" {
		token = "claude-proxy"
	}
	fmt.Println("# PowerShell")
	fmt.Printf("$env:ANTHROPIC_BASE_URL   = \"%s\"\n", base)
	fmt.Printf("$env:ANTHROPIC_AUTH_TOKEN = \"%s\"\n", token)
	fmt.Printf("$env:ANTHROPIC_API_KEY    = \"%s\"\n", token)
	if cfg.DefaultModel != "" {
		fmt.Printf("$env:ANTHROPIC_MODEL      = \"%s\"\n", cfg.DefaultModel)
	}
	fmt.Println()
	fmt.Println("# bash / zsh")
	fmt.Printf("export ANTHROPIC_BASE_URL=\"%s\"\n", base)
	fmt.Printf("export ANTHROPIC_AUTH_TOKEN=\"%s\"\n", token)
	fmt.Printf("export ANTHROPIC_API_KEY=\"%s\"\n", token)
	if cfg.DefaultModel != "" {
		fmt.Printf("export ANTHROPIC_MODEL=\"%s\"\n", cfg.DefaultModel)
	}
}

func runStatus() {
	cfg := mustConfig()
	url := "http://" + cfg.Addr() + "/health"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("claude-proxy is NOT running at %s\n", cfg.Addr())
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Printf("claude-proxy is running at %s\n", cfg.Addr())
	if cfg.RelayMode || cfg.RelayBaseURL != "" {
		dir, derr := executableDir()
		if derr != nil {
			fmt.Printf("licence status: unknown\n")
			return
		}
		if cached, cerr := licensing.DebugStatus(dir); cerr == nil {
			fmt.Printf("licence status: activated\n")
			if cached.HWID != "" {
				fmt.Printf("machine status: registered\n")
			}
			return
		}
	}
	fmt.Printf("licence status: not available\n")
}

func shutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `claude-proxy - GoRouter -> Claude bridge for Trae and Claude Code

Usage:
  claude-proxy [command] [flags]

Commands:
  serve            Run the proxy (default)
  claude [args]    Start the proxy and launch Claude Code wired to it
  env              Print shell exports to point Claude Code at the proxy
  status           Check whether the proxy is running
  version          Print the version
  help             Show this help

Serve flags:
  --port <n>          Override PORT
  --host <addr>       Override HOST
  --auth-token <key>  Override AUTH_TOKEN (client auth)
  --verbose           Debug logging

Configuration is read from environment variables and an optional .env file.
Use the tray app's Settings menu to change it without editing files.
`)
}
