package bridge

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Server is the running bridge: live (atomic) config, the upstream HTTP client,
// a small model-list cache, and lightweight client/request telemetry for the
// admin endpoints.
type Server struct {
	cfgPtr    atomic.Pointer[Config]
	client    *http.Client
	version   string
	startTime time.Time

	mu          sync.Mutex
	modelsCache []string
	modelsExp   time.Time

	totalRequests  atomic.Int64
	upstreamErrors atomic.Int64
	streamCount    atomic.Int64
	clientsMu      sync.Mutex
	clients        map[string]*clientStat

	featuresMu sync.Mutex
	features   map[string]ModelFeatures
}

type clientStat struct {
	ID        string    `json:"id"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Requests  int64     `json:"requests"`
}

// track records a request against a client identity (a short fingerprint of the
// client key, or the remote address for keyless local calls).
func (s *Server) track(id string) {
	s.totalRequests.Add(1)
	if id == "" {
		id = "anonymous"
	}
	now := time.Now()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if s.clients == nil {
		s.clients = map[string]*clientStat{}
	}
	c := s.clients[id]
	if c == nil {
		c = &clientStat{ID: id, FirstSeen: now}
		s.clients[id] = c
	}
	c.LastSeen = now
	c.Requests++
}

func (s *Server) snapshotClients() []clientStat {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	out := make([]clientStat, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, *c)
	}
	return out
}
