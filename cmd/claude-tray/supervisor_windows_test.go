//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestSupervisor(t *testing.T, exePath string) *supervisor {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &supervisor{
		dir:         dir,
		exePath:     exePath,
		consolePath: filepath.Join(dir, "logs", consoleLog),
		restart:     make(chan struct{}, 1),
		quit:        make(chan struct{}),
	}
}

func TestConsoleLogSeparatesRuns(t *testing.T) {
	whoami := filepath.Join(os.Getenv("SystemRoot"), "System32", "whoami.exe")
	if _, err := os.Stat(whoami); err != nil {
		t.Skipf("no stub executable available: %v", err)
	}
	s := newTestSupervisor(t, whoami)

	for range 2 {
		exited := s.start()
		if exited == nil {
			t.Fatal("stub child did not spawn")
		}
		<-exited
	}

	data, err := os.ReadFile(s.consolePath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if got := strings.Count(log, "===== proxy started"); got != 2 {
		t.Errorf("start markers = %d, want 2", got)
	}
	if got := strings.Count(log, "===== proxy stopped"); got != 2 {
		t.Errorf("stop markers = %d, want 2", got)
	}
	if got := strings.Count(log, consoleLogSeparator); got != 2 {
		t.Errorf("separators = %d, want 2", got)
	}
	if first, second := strings.Index(log, consoleLogSeparator), strings.LastIndex(log, "===== proxy started"); first > second {
		t.Error("separator was not written before the following run started")
	}
}

func TestTriggerRestartQueuesSignalAndStopsChild(t *testing.T) {
	s := newTestSupervisor(t, filepath.Join(os.Getenv("SystemRoot"), "System32", "whoami.exe"))

	s.triggerRestart()

	select {
	case <-s.restart:
	default:
		t.Fatal("restart was not queued, so the loop would never relaunch")
	}
	if !s.stopping.Load() {
		t.Error("stopping was not set, so a natural exit would not be treated as intended")
	}
}
