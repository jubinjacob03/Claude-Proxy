//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	updateRepo         = "jubinjacob03/claude-proxy"
	updateAssetName    = "Claude-Proxy-Setup.exe"
	updateChecksumName = "SHA256SUMS.txt"

	updateCheckInterval   = 60 * time.Minute
	updateStartupDelay    = 90 * time.Second
	updateHTTPTimeout     = 15 * time.Minute
	updateChecksumTimeout = 30 * time.Second
	updateMaxAssetBytes   = 200 << 20

	updateCheckWindow = 6 * time.Second
	updateApplyBudget = 15 * time.Minute
	updateMaxAttempts = 2

	updateStagingDir = "update-staging"
	updateMarkerName = "pending.json"

	defaultUpdateAPIBase = "https://api.github.com"
	defaultUpdateWebBase = "https://github.com"
)

var appVersion = "dev"

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

type pendingUpdate struct {
	Version  string `json:"version"`
	File     string `json:"file"`
	SHA256   string `json:"sha256"`
	Attempts int    `json:"attempts"`
}

type updater struct {
	stagingDir string
	client     *http.Client
	metaClient *http.Client
	tagClient  *http.Client
	apiBase    string
	webBase    string

	mu            sync.Mutex
	stagedVersion string
	stagedPath    string

	onStaged func(version string)
	onStatus func(percent int, text string)
}

func (u *updater) status(percent int, text string) {
	if u.onStatus != nil {
		u.onStatus(percent, text)
	}
}

func newUpdater(installDir string, onStaged func(version string)) *updater {
	metaTransport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
	}
	noFollow := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &updater{
		stagingDir: filepath.Join(installDir, updateStagingDir),
		client:     &http.Client{Timeout: updateHTTPTimeout},
		metaClient: &http.Client{Transport: metaTransport, Timeout: updateCheckWindow},
		tagClient:  &http.Client{Transport: metaTransport, Timeout: updateCheckWindow, CheckRedirect: noFollow},
		apiBase:    updateAPIBase(),
		webBase:    updateWebBase(),
		onStaged:   onStaged,
	}
}

func updateAPIBase() string {
	v := strings.TrimSpace(os.Getenv("CLAUDE_UPDATE_API"))
	if v == "" {
		return defaultUpdateAPIBase
	}
	if !isLoopbackURL(v) {
		return defaultUpdateAPIBase
	}
	return strings.TrimRight(v, "/")
}

func updateWebBase() string {
	v := strings.TrimSpace(os.Getenv("CLAUDE_UPDATE_API"))
	if v == "" || !isLoopbackURL(v) {
		return defaultUpdateWebBase
	}
	return strings.TrimRight(v, "/")
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func updateInterval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("CLAUDE_UPDATE_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 5*time.Second {
			return d
		}
	}
	return updateCheckInterval
}

func updateFirstDelay() time.Duration {
	if iv := updateInterval(); iv < 5*time.Minute {
		return 5 * time.Second
	}
	return updateStartupDelay
}

func autoUpdateEnabled(envPath string) bool {
	switch strings.ToLower(strings.TrimSpace(getEnvValue(envPath, "AUTO_UPDATE"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

func updatable() bool {
	return parseVersion(appVersion) != nil
}

func (u *updater) markerPath() string {
	return filepath.Join(u.stagingDir, updateMarkerName)
}

func (u *updater) readPending() (*pendingUpdate, bool) {
	data, err := os.ReadFile(u.markerPath())
	if err != nil {
		return nil, false
	}
	var p pendingUpdate
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, false
	}
	if p.Version == "" || p.File == "" {
		return nil, false
	}
	return &p, true
}

func (u *updater) writePending(p *pendingUpdate) error {
	if err := os.MkdirAll(u.stagingDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(u.markerPath(), data, 0o644)
}

func (u *updater) clearPending() {
	_ = os.Remove(u.markerPath())
}

func (u *updater) pendingInstaller() (*pendingUpdate, string, bool) {
	p, ok := u.readPending()
	if !ok {
		return nil, "", false
	}
	if compareVersions(parseVersion(p.Version), parseVersion(appVersion)) <= 0 {
		return p, "", false
	}
	path := filepath.Join(u.stagingDir, p.File)
	if _, err := os.Stat(path); err != nil {
		return p, "", false
	}
	return p, path, true
}

func (u *updater) applyStartupUpdate(ctx context.Context) bool {
	pending, pendingPath, pendingReady := u.pendingInstaller()
	if !pendingReady && pending != nil {
		logUpdate("clearing stale update flag for %s (running %s)", pending.Version, appVersion)
		u.clearPending()
		u.pruneStaging("")
		return false
	}

	if pendingReady {
		p, path := pending, pendingPath
		if p.Attempts >= updateMaxAttempts {
			logUpdate("update to %s failed %d times; abandoning it", p.Version, p.Attempts)
			u.clearPending()
			u.pruneStaging("")
			return false
		}
		p.Attempts++
		if err := u.writePending(p); err != nil {
			logUpdate("could not record update attempt: %v", err)
		}
		u.setStaged(p.Version, path)
		logUpdate("installing flagged update %s before startup (attempt %d)", p.Version, p.Attempts)

		u.status(splashIndeterminate, "Installing update v"+p.Version+"...")
		if err := u.applyNow(true); err != nil {
			logUpdate("startup install failed: %v", err)
			u.status(splashIndeterminate, "Starting Claude Proxy...")
			return false
		}
		return true
	}

	decisionCtx, cancelDecision := context.WithTimeout(ctx, updateCheckWindow)
	defer cancelDecision()
	started := time.Now()
	tag, err := u.latestTag(decisionCtx)
	if err != nil {
		logUpdate("version check gave up after %v (%v); starting normally",
			time.Since(started).Round(time.Millisecond), err)
		return false
	}
	if compareVersions(parseVersion(tag), parseVersion(appVersion)) <= 0 {
		logUpdate("up to date at %s (checked in %v)",
			appVersion, time.Since(started).Round(time.Millisecond))
		return false
	}

	version := normalizeVersion(tag)
	rel, err := u.fetchLatest(decisionCtx)
	if err != nil {
		logUpdate("update %s found but its details could not be read (%v); starting without it",
			version, err)
		return false
	}
	if !releaseIsNewer(rel) {
		return false
	}
	logUpdate("update %s found; holding startup until it is installed", version)

	u.status(0, "Downloading update v"+version+"...")
	applyCtx, cancel := context.WithTimeout(ctx, updateApplyBudget)
	defer cancel()

	if err := u.stageRelease(applyCtx, rel); err != nil {
		logUpdate("update %s could not be prepared (%v); starting without it", version, err)
		u.status(splashIndeterminate, "Starting Claude Proxy...")
		return false
	}
	if _, _, ok := u.staged(); !ok {
		u.status(splashIndeterminate, "Starting Claude Proxy...")
		return false
	}
	logUpdate("installing freshly downloaded update %s before startup", version)
	u.status(splashIndeterminate, "Installing update v"+version+"...")
	if err := u.applyNow(true); err != nil {
		logUpdate("startup install failed: %v", err)
		u.status(splashIndeterminate, "Starting Claude Proxy...")
		return false
	}
	return true
}

func (u *updater) run(ctx context.Context) {
	if !updatable() {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(updateFirstDelay()):
	}

	interval := updateInterval()
	for {
		if err := u.checkOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logUpdate("check failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (u *updater) checkOnce(ctx context.Context) error {
	decisionCtx, cancel := context.WithTimeout(ctx, updateCheckWindow)
	if tag, err := u.latestTag(decisionCtx); err == nil {
		if compareVersions(parseVersion(tag), parseVersion(appVersion)) <= 0 {
			cancel()
			return nil
		}
	}
	rel, err := u.fetchLatest(decisionCtx)
	cancel()
	if err != nil {
		return err
	}
	return u.stageRelease(ctx, rel)
}

func (u *updater) latestTag(ctx context.Context) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, updateCheckWindow)
	defer cancel()

	endpoint := u.webBase + "/" + updateRepo + "/releases/latest"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "claude-tray/"+appVersion)
	req.Header.Set("Accept", "text/html")

	resp, err := u.tagClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("release page returned %d, expected a redirect", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("redirect carried no Location header")
	}
	tag := loc
	if i := strings.LastIndex(loc, "/"); i >= 0 {
		tag = loc[i+1:]
	}
	if parseVersion(tag) == nil {
		return "", fmt.Errorf("unusable tag %q in redirect", tag)
	}
	return tag, nil
}

func (u *updater) fetchLatest(ctx context.Context) (*ghRelease, error) {
	metaCtx, cancel := context.WithTimeout(ctx, updateCheckWindow)
	defer cancel()
	return u.latestRelease(metaCtx)
}

func releaseIsNewer(rel *ghRelease) bool {
	if rel == nil || rel.Draft || rel.Prerelease {
		return false
	}
	remote := parseVersion(rel.TagName)
	if remote == nil {
		return false
	}
	return compareVersions(remote, parseVersion(appVersion)) > 0
}

func (u *updater) stageRelease(ctx context.Context, rel *ghRelease) error {
	if rel.Draft || rel.Prerelease {
		return nil
	}

	remote := parseVersion(rel.TagName)
	if remote == nil {
		return fmt.Errorf("unparsable release tag %q", rel.TagName)
	}
	if compareVersions(remote, parseVersion(appVersion)) <= 0 {
		return nil
	}
	version := normalizeVersion(rel.TagName)

	if pending, path, ok := u.pendingInstaller(); ok && pending.Version == version {
		u.setStaged(version, path)
		return nil
	}

	asset, ok := findAsset(rel.Assets, updateAssetName)
	if !ok {
		return fmt.Errorf("release %s has no %s asset", rel.TagName, updateAssetName)
	}
	if asset.Size > updateMaxAssetBytes {
		return fmt.Errorf("release asset is too large: %d bytes exceeds %d", asset.Size, updateMaxAssetBytes)
	}

	stageCtx, cancel := context.WithTimeout(ctx, updateApplyBudget)
	defer cancel()

	logUpdate("release %s is newer than %s; downloading %s (%d bytes)",
		rel.TagName, appVersion, asset.Name, asset.Size)

	wantSum, err := u.expectedChecksum(stageCtx, rel, asset.Name)
	if err != nil {
		return err
	}
	path, err := u.download(stageCtx, version, asset, wantSum)
	if err != nil {
		return err
	}

	u.setStaged(version, path)
	if err := u.writePending(&pendingUpdate{
		Version: version,
		File:    filepath.Base(path),
		SHA256:  wantSum,
	}); err != nil {
		logUpdate("could not write update flag: %v", err)
	}
	u.pruneStaging(filepath.Base(path))
	logUpdate("staged %s at %s and flagged for install", version, path)

	if u.onStaged != nil {
		u.onStaged(version)
	}
	return nil
}

func (u *updater) latestRelease(ctx context.Context) (*ghRelease, error) {
	endpoint := u.apiBase + "/repos/" + updateRepo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "claude-tray/"+appVersion)

	resp, err := u.metaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, errors.New("release has no tag")
	}
	return &rel, nil
}

func (u *updater) expectedChecksum(ctx context.Context, rel *ghRelease, name string) (string, error) {
	sums, ok := findAsset(rel.Assets, updateChecksumName)
	if !ok {
		return "", fmt.Errorf("release %s has no %s; refusing to install unverified",
			rel.TagName, updateChecksumName)
	}
	checksumCtx, cancel := context.WithTimeout(ctx, updateChecksumTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(checksumCtx, http.MethodGet, sums.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "claude-tray/"+appVersion)
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum download returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	sum, ok := parseChecksums(string(data), name)
	if !ok {
		return "", fmt.Errorf("%s has no entry for %s", updateChecksumName, name)
	}
	return sum, nil
}

func (u *updater) download(ctx context.Context, version string, asset ghAsset, wantSum string) (string, error) {
	if err := os.MkdirAll(u.stagingDir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(u.stagingDir, "Claude-Proxy-Setup-"+sanitizeFileToken(version)+".exe")
	part := final + ".part"
	_ = os.Remove(part)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "claude-tray/"+appVersion)

	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asset download returned %d", resp.StatusCode)
	}

	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	writers := []io.Writer{f, hasher}
	if u.onStatus != nil {
		writers = append(writers, &progressMeter{total: asset.Size, label: "Downloading update v" + version, report: u.onStatus})
	}
	written, err := io.Copy(io.MultiWriter(writers...), io.LimitReader(resp.Body, updateMaxAssetBytes+1))
	closeErr := f.Close()
	if err != nil {
		os.Remove(part)
		return "", err
	}
	if closeErr != nil {
		os.Remove(part)
		return "", closeErr
	}
	if written > updateMaxAssetBytes {
		os.Remove(part)
		return "", fmt.Errorf("asset exceeds %d bytes", updateMaxAssetBytes)
	}
	if asset.Size > 0 && written != asset.Size {
		os.Remove(part)
		return "", fmt.Errorf("size mismatch: got %d, want %d", written, asset.Size)
	}

	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, wantSum) {
		os.Remove(part)
		return "", fmt.Errorf("checksum mismatch: got %s, want %s", got, wantSum)
	}
	if err := verifyPEFile(part); err != nil {
		os.Remove(part)
		return "", err
	}

	_ = os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		os.Remove(part)
		return "", err
	}
	return final, nil
}

type progressMeter struct {
	total  int64
	label  string
	report func(percent int, text string)

	written int64
	lastAt  time.Time
	lastPct int
}

func (m *progressMeter) Write(p []byte) (int, error) {
	n := len(p)
	m.written += int64(n)
	if m.report == nil {
		return n, nil
	}

	percent := splashIndeterminate
	if m.total > 0 {
		percent = int(m.written * 100 / m.total)
		if percent > 100 {
			percent = 100
		}
	}
	done := m.total > 0 && m.written >= m.total
	now := time.Now()
	if done && m.lastPct == 100 {
		return n, nil
	}
	if !done && !m.lastAt.IsZero() && now.Sub(m.lastAt) < 200*time.Millisecond {
		return n, nil
	}
	m.lastAt, m.lastPct = now, percent

	text := m.label + "..."
	if m.total > 0 {
		displayed := min(m.written, m.total)
		text = fmt.Sprintf("%s  %d%%  (%s of %s)", m.label, percent, humanMB(displayed), humanMB(m.total))
	}
	m.report(percent, text)
	return n, nil
}

func (u *updater) pruneStaging(keep string) {
	entries, err := os.ReadDir(u.stagingDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == keep || e.Name() == updateMarkerName {
			continue
		}
		_ = os.Remove(filepath.Join(u.stagingDir, e.Name()))
	}
}

func (u *updater) setStaged(version, path string) {
	u.mu.Lock()
	u.stagedVersion, u.stagedPath = version, path
	u.mu.Unlock()
}

func (u *updater) staged() (string, string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stagedPath == "" {
		return "", "", false
	}
	return u.stagedVersion, u.stagedPath, true
}

func (u *updater) applyNow(relaunch bool) error {
	version, path, ok := u.staged()
	if !ok {
		return errors.New("no update staged")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("staged installer missing: %w", err)
	}
	logUpdate("applying %s from %s (relaunch=%v)", version, path, relaunch)
	return launchInstaller(path, relaunch)
}

func verifyPEFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [2]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("cannot read header: %w", err)
	}
	if magic[0] != 'M' || magic[1] != 'Z' {
		return errors.New("downloaded file is not a Windows executable")
	}
	return nil
}

func findAsset(assets []ghAsset, name string) (ghAsset, bool) {
	for _, a := range assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return ghAsset{}, false
}

func parseChecksums(data, want string) (string, bool) {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if strings.EqualFold(filepath.Base(name), want) {
			sum := fields[0]
			if len(sum) != 64 {
				return "", false
			}
			if _, err := hex.DecodeString(sum); err != nil {
				return "", false
			}
			return sum, true
		}
	}
	return "", false
}

func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexAny(s, "+"); i >= 0 {
		s = s[:i]
	}
	return s
}

func parseVersion(s string) []int {
	s = normalizeVersion(s)
	if s == "" || strings.ContainsAny(s, "-") {
		return nil
	}
	fields := strings.Split(s, ".")
	if len(fields) == 0 || len(fields) > 4 {
		return nil
	}
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			return nil
		}
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func compareVersions(a, b []int) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func sanitizeFileToken(s string) string {
	s = filepath.Base(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "asset"
	}
	return out
}
