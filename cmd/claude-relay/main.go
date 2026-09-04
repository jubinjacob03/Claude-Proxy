// Command claude-relay is the licensed server side of the proxy: it holds the
// pooled upstream keys, validates licences, and meters usage.
//
// It is deliberately a separate binary from claude-proxy. The proxy ships to
// users; this never does.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"claude-proxy/internal/license"
	"claude-proxy/internal/logx"
	"claude-proxy/internal/relay"
)

const version = "0.1.0"

func main() {
	logx.SetLevel(logx.ParseLevel(env("LOG_LEVEL", "info")))

	cfg := relay.Config{
		Addr:          env("RELAY_ADDR", ":8080"),
		DataDir:       env("RELAY_DATA_DIR", "data"),
		TokenSecret:   os.Getenv("RELAY_TOKEN_SECRET"),
		AdminToken:    os.Getenv("RELAY_ADMIN_TOKEN"),
		ClaudeBaseURL: strings.TrimRight(env("UPSTREAM_BASE_URL", "https://gorouter.app"), "/"),
		DefaultQuota:  license.Money(envInt("DEFAULT_QUOTA_CENTS", 7000)),
	}

	// Without a stable secret every client token would be invalidated on
	// restart, so refuse to start rather than silently log everyone out.
	if cfg.TokenSecret == "" {
		fmt.Fprintln(os.Stderr, "RELAY_TOKEN_SECRET is required (use a long random string)")
		os.Exit(1)
	}
	if cfg.AdminToken == "" {
		logx.Warn("RELAY_ADMIN_TOKEN is not set; the admin API is disabled")
	}

	// Pooled upstream secrets are encrypted with this; losing it makes them
	// unreadable, so it is required rather than defaulted.
	dbKey := os.Getenv("RELAY_DB_KEY")
	if dbKey == "" {
		fmt.Fprintln(os.Stderr, "RELAY_DB_KEY is required (encrypts pooled API keys at rest)")
		os.Exit(1)
	}

	store, err := license.Open(cfg.DataDir, []byte(dbKey))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer store.Close()

	if len(os.Args) > 1 {
		if err := runCommand(store, cfg, os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           relay.New(cfg, store).Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	logx.Info("claude-relay %s", version)
	logx.Info("  listening: %s", cfg.Addr)
	logx.Info("  data dir:  %s", cfg.DataDir)
	logx.Info("  upstream:  %s", cfg.ClaudeBaseURL)
	logx.Info("  default quota: %s", cfg.DefaultQuota)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logx.Error("serve: %v", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	logx.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// runCommand covers the operator tasks that are easier from a shell than from
// the website, most importantly seeding the very first pooled key.
func runCommand(store *license.Store, cfg relay.Config, args []string) error {
	switch args[0] {
	case "mint":
		count := 1
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("mint: expected a positive count, got %q", args[1])
			}
			count = n
		}
		for i := 0; i < count; i++ {
			l, err := store.CreateLicense(cfg.DefaultQuota, "minted from cli")
			if err != nil {
				return err
			}
			fmt.Printf("%s  %s\n", l.Key, l.QuotaCents)
		}
		return nil

	case "add-key":
		if len(args) < 4 {
			return fmt.Errorf("usage: claude-relay add-key <provider: claude> <secret> <balance-dollars> [label] [pool-group]")
		}
		dollars, err := strconv.ParseFloat(args[3], 64)
		if err != nil || dollars <= 0 {
			return fmt.Errorf("add-key: expected a positive balance in dollars, got %q", args[3])
		}
		label := "pooled"
		if len(args) > 4 {
			label = args[4]
		}
		poolGroup := "default"
		if len(args) > 5 {
			poolGroup = args[5]
		}
		k, err := store.AddPoolKeyInGroup(label, args[2], args[1], poolGroup, license.Money(dollars*100))
		if err != nil {
			return err
		}
		fmt.Printf("added %s key %s (%s) in group %s with %s\n", k.Provider, k.ID, k.Label, k.PoolGroup, k.BalanceCents)
		return nil

	case "pool":
		for _, k := range store.ListPoolKeys() {
			state := "active"
			if !k.Active {
				state = "retired"
			}
			fmt.Printf("%s  %-8s %-7s %-10s %s left of %s  %s\n",
				k.ID, state, k.Provider, k.PoolGroup, k.Remaining(), k.BalanceCents, k.Label)
		}
		return nil

	case "licenses":
		for _, l := range store.List() {
			state := "active"
			if !l.Active {
				state = "paused"
			}
			fmt.Printf("%s  %-8s spent %s of %s  hwid=%t  %s\n",
				l.ID, state, l.SpentCents, l.QuotaCents, l.Bound(), l.Note)
		}
		return nil

	default:
		return fmt.Errorf("unknown command %q (try: mint, add-key, pool, licenses)", args[0])
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int64) int64 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
