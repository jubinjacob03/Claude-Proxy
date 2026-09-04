//go:build windows

package main

import "testing"

func TestCapDur(t *testing.T) {
	if got := capDur(5, 3); got != 3 {
		t.Fatalf("capDur mismatch")
	}
}
