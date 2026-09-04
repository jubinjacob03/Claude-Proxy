package bridge

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"claude-proxy/internal/logx"
)

// NewServer builds a bridge server around a loaded configuration.
func NewServer(cfg *Config, version string) *Server {
	s := &Server{
		client:    newHTTPClient(),
		version:   version,
		startTime: time.Now(),
		clients:   map[string]*clientStat{},
		features:  map[string]ModelFeatures{},
	}
	s.cfgPtr.Store(cfg)
	return s
}

// cfg returns the current live configuration snapshot.
func (s *Server) cfg() *Config { return s.cfgPtr.Load() }

// setConfig atomically swaps in a new configuration (used by POST /config) and
// invalidates the model cache.
func (s *Server) setConfig(c *Config) {
	s.cfgPtr.Store(c)
	s.mu.Lock()
	s.modelsCache = nil
	s.modelsExp = time.Time{}
	s.mu.Unlock()
}

// Handler builds the routed HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/v1/messages/count_tokens", s.handleCountTokens)
	mux.HandleFunc("/v1/chat/completions", s.handleCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/models", s.handleModelsCompact)

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/admin/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/admin/stats", s.handleAdminStats)
	mux.HandleFunc("/admin/clients", s.handleAdminClients)
	mux.HandleFunc("/config", s.handleConfig)
	mux.HandleFunc("/features", s.handleFeatures)
	mux.HandleFunc("/stop", s.handleStop)

	mux.HandleFunc("/", s.handleUnknown)
	return withRecover(s.withMiddleware(mux))
}

// Start binds the listener and serves in the background. It returns the running
// http.Server (call Shutdown to stop) or an error if the port is unavailable.
func (s *Server) Start() (*http.Server, error) {
	c := s.cfg()
	srv := &http.Server{
		Addr:              c.Addr(),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return nil, err
	}
	s.banner(c)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logx.Error("serve error: %v", err)
		}
	}()
	return srv, nil
}

// Run starts the server and blocks until SIGINT/SIGTERM, then drains in-flight
// requests and shuts down cleanly.
func (s *Server) Run() error {
	srv, err := s.Start()
	if err != nil {
		return err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	logx.Info("shutting down; draining in-flight requests…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func (s *Server) banner(c *Config) {
	logx.Info("claude-proxy %s", s.version)
	logx.Info("  listening:  http://%s", c.Addr())
	logx.Info("  upstream:   %s (%s)", c.UpstreamBaseURL, c.UpstreamFormat)
	logx.Info("  auth token: %s", logx.Redact(c.AuthToken))
	logx.Info("  upstream key: %s", logx.Redact(c.UpstreamAPIKey))
}
