// Package ansi provides minimal terminal colouring for console output. Colour is
// disabled when NO_COLOR is set or when the caller turns it off (for example for
// JSON logs), so piped output stays clean.
package ansi

import (
	"os"
	"sync/atomic"
)

var enabled atomic.Bool

func init() {
	_, noColor := os.LookupEnv("NO_COLOR")
	enabled.Store(!noColor)
}

// SetEnabled turns colouring on or off globally.
func SetEnabled(on bool) { enabled.Store(on) }

func wrap(code, s string) string {
	if !enabled.Load() {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Bold(s string) string   { return wrap("1", s) }
func Grey(s string) string   { return wrap("90", s) }
func Red(s string) string    { return wrap("31", s) }
func Green(s string) string  { return wrap("32", s) }
func Yellow(s string) string { return wrap("33", s) }
func Violet(s string) string { return wrap("35", s) }
func Cyan(s string) string   { return wrap("36", s) }
