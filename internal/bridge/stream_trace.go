package bridge

import (
	"strings"
	"sync/atomic"

	"claude-proxy/internal/logx"
)

// streamTrace records every frame crossing the proxy.
// TEMPORARY: on by default while the editor integration is being diagnosed. It
// logs full conversation content, so switch the default back off before this
// ships more widely.
var streamTrace atomic.Bool

func setStreamTrace(on bool) { streamTrace.Store(on) }

func tracingStream() bool { return streamTrace.Load() }

// traceIn records a frame exactly as the upstream sent it.
func traceIn(turn uint64, line string) {
	if !tracingStream() {
		return
	}
	logx.Info("stream[%d] <- upstream: %s", turn, clipTrace(line))
}

// traceOut records a frame exactly as the client will receive it, with why it
// was produced, so a stalled turn can be read back frame by frame.
func traceOut(turn uint64, reason, line string) {
	if !tracingStream() {
		return
	}
	logx.Info("stream[%d] -> client [%s]: %s", turn, reason, clipTrace(line))
}

func traceNote(turn uint64, format string, args ...any) {
	if !tracingStream() {
		return
	}
	logx.Info("stream[%d] "+format, append([]any{turn}, args...)...)
}

var streamTurnSeq atomic.Uint64

func nextStreamTurn() uint64 { return streamTurnSeq.Add(1) }

func clipTrace(s string) string {
	s = strings.TrimRight(s, "\n")
	const max = 4000
	if len(s) > max {
		return s[:max] + "…[truncated]"
	}
	return s
}
