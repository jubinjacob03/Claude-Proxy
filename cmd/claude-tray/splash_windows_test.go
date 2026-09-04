//go:build windows

package main

import "testing"

func TestSplashStub(t *testing.T) {
	s := newSplash("")
	s.set(splashIndeterminate, "")
	s.close()
}
