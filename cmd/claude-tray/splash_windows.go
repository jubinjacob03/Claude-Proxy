//go:build windows

package main

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"
)

const splashIndeterminate = -1

type splash struct{}

func newSplash(_ string) *splash      { return &splash{} }
func (s *splash) set(_ int, _ string) {}
func (s *splash) close()              {}

func waitProxyReady(dir string, timeout time.Duration) {
	port := getEnvValue(filepath.Join(dir, envFileName), "PORT")
	if port == "" {
		port = "3001"
	}
	endpoint := "http://127.0.0.1:" + port + "/health"
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))
			response.Body.Close()
			return
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		time.Sleep(min(250*time.Millisecond, remaining))
	}
}

func humanMB(bytes int64) string {
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
}
