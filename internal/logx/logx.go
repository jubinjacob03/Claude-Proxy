package logx

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelOff
)

func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info", "":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	case "off", "none", "silent":
		return LevelOff
	default:
		return LevelInfo
	}
}

// LevelName is the inverse of ParseLevel.
func LevelName(l Level) string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	case LevelOff:
		return "off"
	default:
		return "info"
	}
}

type Format int

const (
	FormatText Format = iota
	FormatJSON
)

// SetFormat switches the log line format between text and JSON.
func SetFormat(name string) {
	mu.Lock()
	if strings.EqualFold(strings.TrimSpace(name), "json") {
		outputFormat = FormatJSON
	} else {
		outputFormat = FormatText
	}
	mu.Unlock()
}

var (
	mu           sync.Mutex
	level        = LevelInfo
	outputFormat = FormatText
)

func SetLevel(l Level) {
	mu.Lock()
	level = l
	mu.Unlock()
}

func Enabled(l Level) bool {
	mu.Lock()
	defer mu.Unlock()
	return l >= level && level != LevelOff
}

func logf(l Level, tag, format string, args ...any) {
	if !Enabled(l) {
		return
	}
	msg := fmt.Sprintf(format, args...)

	mu.Lock()
	defer mu.Unlock()

	var line string
	if outputFormat == FormatJSON {
		b, _ := json.Marshal(map[string]string{
			"ts":    time.Now().Format(time.RFC3339Nano),
			"level": strings.ToLower(tag),
			"msg":   msg,
		})
		line = string(b) + "\n"
	} else {
		line = fmt.Sprintf("%s %-5s %s\n", time.Now().Format("15:04:05.000"), tag, msg)
	}

	if l >= LevelWarn {
		_, _ = os.Stderr.WriteString(line)
	} else {
		_, _ = os.Stdout.WriteString(line)
	}
}

func Debug(format string, args ...any) { logf(LevelDebug, "DEBUG", format, args...) }
func Info(format string, args ...any)  { logf(LevelInfo, "INFO", format, args...) }
func Warn(format string, args ...any)  { logf(LevelWarn, "WARN", format, args...) }
func Error(format string, args ...any) { logf(LevelError, "ERROR", format, args...) }

// Redact keeps the shape of a secret visible for debugging without leaking it.
func Redact(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "(none)"
	}
	if len(secret) <= 8 {
		return "****"
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}
