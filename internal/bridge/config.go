package bridge

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"claude-proxy/internal/logx"
)

type UpstreamFormat string

const (
	FormatOpenAI    UpstreamFormat = "openai"
	FormatAnthropic UpstreamFormat = "anthropic"
)

type Config struct {
	Host string
	Port int

	UpstreamBaseURL string
	UpstreamFormat  UpstreamFormat
	UpstreamAPIKey  string

	// RelayBaseURL and LicenseKey drive one-time activation against the
	// licence server; see internal/licensing. RelayMode is never read from
	// env - it is set to true by main.go only after activation succeeds.
	RelayBaseURL string
	LicenseKey   string
	RelayMode    bool

	AuthToken    string
	DefaultModel string

	ModelMap         map[string]string
	DefaultMaxTokens int

	StreamIdlePing time.Duration
	RequestTimeout time.Duration

	LogLevel  logx.Level
	LogFormat string
	LogBodies bool

	// EnvPath is where POST /config persists changes; not user-configurable.
	EnvPath string
}

// Clone returns a deep copy safe to mutate without affecting the live
func (c *Config) Clone() *Config {
	cp := *c
	cp.ModelMap = make(map[string]string, len(c.ModelMap))
	for k, v := range c.ModelMap {
		cp.ModelMap[k] = v
	}
	return &cp
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// MapModel resolves a client-supplied model name to the upstream model id using
// MODEL_MAP, falling back to a "*" catch-all, then to the original name.
func (c *Config) MapModel(model string) string {
	if c.ModelMap == nil {
		return model
	}
	if to, ok := c.ModelMap[model]; ok {
		return to
	}
	if to, ok := c.ModelMap["*"]; ok {
		return to
	}
	return model
}

// Load reads configuration from the environment, layering an optional .env file
// located next to the executable (and the current working directory) underneath
// real environment variables.
func Load() (*Config, error) {
	env := newEnv()

	c := &Config{
		Host:             env.str("HOST", "127.0.0.1"),
		Port:             env.intVal("PORT", 3001),
		UpstreamBaseURL:  strings.TrimRight(env.str("UPSTREAM_BASE_URL", "https://gorouter.app"), "/"),
		UpstreamFormat:   UpstreamFormat(strings.ToLower(env.str("UPSTREAM_FORMAT", "anthropic"))),
		UpstreamAPIKey:   env.str("UPSTREAM_API_KEY", ""),
		AuthToken:        env.str("AUTH_TOKEN", ""),
		DefaultModel:     env.str("DEFAULT_MODEL", "claude-opus-4-8"),
		ModelMap:         parseModelMap(env.str("MODEL_MAP", "")),
		DefaultMaxTokens: env.intVal("DEFAULT_MAX_TOKENS", 4096),
		StreamIdlePing:   time.Duration(env.intVal("STREAM_IDLE_PING_SECONDS", 15)) * time.Second,
		RequestTimeout:   time.Duration(env.intVal("TIMEOUT", 0)) * time.Millisecond,
		LogLevel:         logx.ParseLevel(env.str("LOG_LEVEL", "info")),
		LogFormat:        strings.ToLower(env.str("LOG_FORMAT", "text")),
		LogBodies:        env.boolVal("LOG_BODIES", false),
		EnvPath:          targetEnvPath(),
		RelayBaseURL:     strings.TrimRight(env.str("RELAY_BASE_URL", "http://127.0.0.1:43211"), "/"),
		LicenseKey:       env.str("LICENSE_KEY", ""),
	}

	if c.UpstreamFormat != FormatOpenAI && c.UpstreamFormat != FormatAnthropic {
		return nil, fmt.Errorf("UPSTREAM_FORMAT must be 'openai' or 'anthropic', got %q", c.UpstreamFormat)
	}
	if c.UpstreamBaseURL == "" {
		return nil, fmt.Errorf("UPSTREAM_BASE_URL is required")
	}
	if c.DefaultMaxTokens <= 0 {
		c.DefaultMaxTokens = 4096
	}

	return c, nil
}

func parseModelMap(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}
	}
	m := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.Index(pair, "=")
		if eq < 0 {
			continue
		}
		from := strings.TrimSpace(pair[:eq])
		to := strings.TrimSpace(pair[eq+1:])
		if from != "" && to != "" {
			m[from] = to
		}
	}
	return m
}

// env layers process environment over a parsed .env file.
type env struct {
	file map[string]string
}

func newEnv() *env {
	e := &env{file: map[string]string{}}
	for _, p := range dotenvPaths() {
		if loaded, ok := parseDotenv(p); ok {
			for k, v := range loaded {
				if _, exists := e.file[k]; !exists {
					e.file[k] = v
				}
			}
			logx.Debug("loaded .env from %s", p)
		}
	}
	return e
}

func (e *env) raw(key string) (string, bool) {
	if v, ok := os.LookupEnv(key); ok {
		return v, true
	}
	if v, ok := e.file[key]; ok {
		return v, true
	}
	return "", false
}

func (e *env) str(key, def string) string {
	if v, ok := e.raw(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (e *env) intVal(key string, def int) int {
	if v, ok := e.raw(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func (e *env) boolVal(key string, def bool) bool {
	if v, ok := e.raw(key); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return def
}

func dotenvPaths() []string {
	var paths []string
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), ".env"))
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(wd, ".env"))
	}
	return paths
}

func parseDotenv(path string) (map[string]string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		if key != "" {
			out[key] = val
		}
	}
	return out, true
}

// targetEnvPath picks where POST /config writes: an existing .env if found,
// otherwise the .env next to the executable.
func targetEnvPath() string {
	paths := dotenvPaths()
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ".env"
}

// Save writes the configuration to EnvPath as a .env file. Runtime changes made
// at runtime survive a restart this way.
func (c *Config) Save() error {
	path := c.EnvPath
	if path == "" {
		path = targetEnvPath()
	}
	var b strings.Builder
	b.WriteString("# Written by claude-proxy. Edit freely; real env vars still win at startup.\n")
	fmt.Fprintf(&b, "HOST=%s\n", c.Host)
	fmt.Fprintf(&b, "PORT=%d\n", c.Port)
	// Under licensing the upstream is the relay and the "key" is a licence
	// token derived from this machine. Writing either back would skip
	// activation on the next start and strand the user if the token rotates,
	// so persist only the settings a direct install owns.
	if !c.RelayMode {
		fmt.Fprintf(&b, "UPSTREAM_BASE_URL=%s\n", c.UpstreamBaseURL)
		fmt.Fprintf(&b, "UPSTREAM_API_KEY=%s\n", c.UpstreamAPIKey)
	}
	fmt.Fprintf(&b, "UPSTREAM_FORMAT=%s\n", c.UpstreamFormat)
	fmt.Fprintf(&b, "AUTH_TOKEN=%s\n", c.AuthToken)
	fmt.Fprintf(&b, "DEFAULT_MODEL=%s\n", c.DefaultModel)
	fmt.Fprintf(&b, "MODEL_MAP=%s\n", ModelMapString(c.ModelMap))
	fmt.Fprintf(&b, "DEFAULT_MAX_TOKENS=%d\n", c.DefaultMaxTokens)
	fmt.Fprintf(&b, "STREAM_IDLE_PING_SECONDS=%d\n", int(c.StreamIdlePing/time.Second))
	fmt.Fprintf(&b, "TIMEOUT=%d\n", int(c.RequestTimeout/time.Millisecond))
	fmt.Fprintf(&b, "LOG_LEVEL=%s\n", logx.LevelName(c.LogLevel))
	fmt.Fprintf(&b, "LOG_FORMAT=%s\n", c.LogFormat)
	fmt.Fprintf(&b, "LOG_BODIES=%t\n", c.LogBodies)

	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// ModelMapString renders a model map as comma-separated from=to pairs.
func ModelMapString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

// ParseModelMap is the exported form used when applying runtime config updates.
func ParseModelMap(raw string) map[string]string {
	return parseModelMap(raw)
}
