package bridge

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"claude-proxy/internal/ansi"
	"claude-proxy/internal/logx"
)

// One console line per request, so a busy terminal stays readable. Per-body
// detail still reaches the log at debug level.

type accessRecord struct {
	started time.Time
	method  string
	path    string
	model   string
	stream  bool
	outcome string
}

type accessKey struct{}

func newAccessRecord(method, path string) *accessRecord {
	return &accessRecord{started: time.Now(), method: method, path: path}
}

// withAccess stashes the record on the request so handlers can annotate it.
func withAccess(r *http.Request, a *accessRecord) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accessKey{}, a))
}

func accessFrom(r *http.Request) *accessRecord {
	if a, ok := r.Context().Value(accessKey{}).(*accessRecord); ok {
		return a
	}
	return nil
}

// annotate records the resolved model and streaming mode for the summary line.
func annotate(r *http.Request, model string, stream bool) {
	if a := accessFrom(r); a != nil {
		a.model = model
		a.stream = stream
	}
}

func note(r *http.Request, outcome string) {
	if a := accessFrom(r); a != nil {
		a.outcome = outcome
	}
}

// done emits the summary line. Labels are dim and values bright, so the eye lands
// on model, status, and timing.
func (a *accessRecord) done(status int, bytesOut int64) {
	var b strings.Builder
	b.Grow(160)
	b.WriteString(ansi.Bold(ansi.Violet(a.method)))
	b.WriteByte(' ')
	b.WriteString(a.path)

	if a.model != "" {
		b.WriteString(ansi.Grey(" model="))
		b.WriteString(ansi.Cyan(a.model))
	}
	if a.stream {
		b.WriteString(ansi.Grey(" stream"))
	}

	paint := statusColour(status)
	b.WriteByte(' ')
	b.WriteString(ansi.Bold(paint(strconv.Itoa(status))))

	b.WriteString(ansi.Grey(" in "))
	b.WriteString(formatDuration(time.Since(a.started)))

	if bytesOut > 0 {
		b.WriteString(ansi.Grey(" out="))
		b.WriteString(formatBytes(bytesOut))
	}
	if a.outcome != "" {
		b.WriteByte(' ')
		b.WriteString(paint("(" + a.outcome + ")"))
	}

	logx.Info("%s", b.String())
}

func statusColour(status int) func(string) string {
	switch {
	case status >= 500:
		return ansi.Red
	case status >= 400:
		return ansi.Yellow
	case status >= 200 && status < 300:
		return ansi.Green
	default:
		return ansi.Cyan
	}
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return strconv.FormatInt(int64(d/time.Microsecond), 10) + "us"
	case d < time.Second:
		return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms"
	default:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	value := float64(n)
	units := [...]string{"KB", "MB", "GB"}
	idx := -1
	for value >= unit && idx < len(units)-1 {
		value /= unit
		idx++
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + units[idx]
}

// statusWriter captures the status code and response size for the access line.
// It implements http.Flusher because SSE streaming depends on flushing; losing
// that interface would buffer every stream.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) statusOrOK() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}
