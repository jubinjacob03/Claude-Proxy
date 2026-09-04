//go:build windows

package main

import "testing"

func TestVersionVar(t *testing.T) {
	if appVersion == "" {
		t.Fatalf("appVersion should not be empty")
	}
}
