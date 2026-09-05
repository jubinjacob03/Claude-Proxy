package license

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store owns every licence, pooled key, and usage row.
//
// Quota arithmetic is a single SQLite transaction guarded by a WHERE clause, so
// two concurrent requests can never both spend the last cent: the second one
// updates zero rows and is refused.
type Store struct {
	db     *sql.DB
	encKey []byte
}

const schema = `
CREATE TABLE IF NOT EXISTS licenses (
  id            TEXT PRIMARY KEY,
  key_hash      TEXT NOT NULL UNIQUE,
  key_hint      TEXT NOT NULL,
  hwid          TEXT NOT NULL DEFAULT '',
  quota_cents   INTEGER NOT NULL,
  spent_cents   INTEGER NOT NULL DEFAULT 0,
  active        INTEGER NOT NULL DEFAULT 1,
  note          TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  bound_at      TEXT,
  last_seen_at  TEXT,
  raw_key       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS pool_keys (
  id             TEXT PRIMARY KEY,
  label          TEXT NOT NULL DEFAULT '',
  provider       TEXT NOT NULL,
	pool_group     TEXT NOT NULL DEFAULT 'default',
  secret_enc     BLOB NOT NULL,
  balance_cents  INTEGER NOT NULL,
  spent_cents    INTEGER NOT NULL DEFAULT 0,
  active         INTEGER NOT NULL DEFAULT 1,
  created_at     TEXT NOT NULL,
  last_used_at   TEXT,
  exhausted_at   TEXT
);

CREATE TABLE IF NOT EXISTS endpoint_profiles (
	name             TEXT PRIMARY KEY,
	claude_base_url  TEXT NOT NULL,
	pool_group       TEXT NOT NULL DEFAULT 'default',
	active           INTEGER NOT NULL DEFAULT 0,
	created_at       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS usage_events (
  id           TEXT PRIMARY KEY,
  license_id   TEXT NOT NULL,
  pool_key_id  TEXT NOT NULL,
  provider     TEXT NOT NULL,
  model        TEXT NOT NULL,
  cost_cents   INTEGER NOT NULL,
  streamed     INTEGER NOT NULL,
  status_code  INTEGER NOT NULL,
  created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_usage_license ON usage_events(license_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_key     ON usage_events(pool_key_id, created_at);
CREATE INDEX IF NOT EXISTS idx_pool_pick     ON pool_keys(provider, pool_group, active);
CREATE INDEX IF NOT EXISTS idx_endpoint_active ON endpoint_profiles(active);
`

// Open prepares the database at dir/license.db. encryptionKey protects pooled
// upstream secrets at rest, so a stolen database file yields nothing usable.
func Open(dir string, encryptionKey []byte) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("license: create data dir: %w", err)
	}
	dsn := "file:" + strings.ReplaceAll(dir, "\\", "/") + "/license.db" +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("license: open database: %w", err)
	}
	// A single connection keeps writes strictly serialised, which is what makes
	// the debit guard reliable. At this scale the lost read parallelism is free.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("license: apply schema: %w", err)
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("license: apply migrations: %w", err)
	}

	sum := sha256.Sum256(encryptionKey)
	return &Store{db: db, encKey: sum[:]}, nil
}

func runMigrations(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE pool_keys ADD COLUMN pool_group TEXT NOT NULL DEFAULT 'default'`,
		`CREATE TABLE IF NOT EXISTS endpoint_profiles (
		  name             TEXT PRIMARY KEY,
		  claude_base_url  TEXT NOT NULL,
		  pool_group       TEXT NOT NULL DEFAULT 'default',
		  active           INTEGER NOT NULL DEFAULT 0,
		  created_at       TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pool_pick ON pool_keys(provider, pool_group, active)`,
		`CREATE INDEX IF NOT EXISTS idx_endpoint_active ON endpoint_profiles(active)`,
		`ALTER TABLE licenses ADD COLUMN raw_key TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			errText := strings.ToLower(err.Error())
			if strings.Contains(errText, "duplicate column") || strings.Contains(errText, "already exists") {
				continue
			}
			return err
		}
	}
	if _, err := db.Exec(`UPDATE pool_keys SET pool_group = 'default' WHERE pool_group = '' OR pool_group IS NULL`); err != nil {
		return err
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// CreateLicense issues a new key. The plaintext key is returned exactly once
// here and never stored; only its hash and a display hint are persisted.
func (s *Store) CreateLicense(quota Money, note string) (*License, error) {
	if quota <= 0 {
		return nil, errors.New("quota must be positive")
	}
	l := &License{
		ID:         NewID(),
		Key:        NewKey(),
		QuotaCents: quota,
		Active:     true,
		Note:       note,
		CreatedAt:  time.Now().UTC(),
	}
	l.RawKey = l.Key
	l.KeyHint = KeyHint(l.Key)

	_, err := s.db.Exec(
		`INSERT INTO licenses (id, key_hash, key_hint, quota_cents, note, created_at, raw_key)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		l.ID, hashKey(l.Key), l.KeyHint, int64(quota), note, iso(l.CreatedAt), l.Key)
	if err != nil {
		return nil, fmt.Errorf("license: create: %w", err)
	}
	return l, nil
}

// Activate binds a licence to a machine on first use and refuses every other
// machine afterwards. Re-activating from the bound machine succeeds, so a
// reinstall does not strand a paying user.
func (s *Store) Activate(key, hwid string) (*License, error) {
	hwid = strings.TrimSpace(hwid)
	if hwid == "" {
		return nil, ErrHWIDMissing
	}
	l, err := s.byKeyHash(hashKey(key))
	if err != nil {
		return nil, err
	}
	if !l.Active {
		return nil, ErrInactive
	}
	if l.Bound() && l.HWID != hwid {
		return nil, ErrHWIDMismatch
	}
	if !l.Bound() {
		now := time.Now().UTC()
		if _, err := s.db.Exec(
			`UPDATE licenses SET hwid = ?, bound_at = ? WHERE id = ? AND hwid = ''`,
			hwid, iso(now), l.ID); err != nil {
			return nil, err
		}
		l.HWID = hwid
		l.BoundAt = &now
	}
	return l, nil
}

// Authorize is the per-request gate: active, right machine, credit remaining.
func (s *Store) Authorize(licenseID, hwid string) (*License, error) {
	l, err := s.Get(licenseID)
	if err != nil {
		return nil, err
	}
	if !l.Active {
		return nil, ErrInactive
	}
	if l.HWID != hwid {
		return nil, ErrHWIDMismatch
	}
	if l.Remaining() <= 0 {
		return nil, ErrExhausted
	}
	return l, nil
}

// Reservation is an amount debited before a request is forwarded.
type Reservation struct {
	EventID   string
	LicenseID string
	PoolKeyID string
	Cost      Money
}

// Reserve debits the licence and the pooled key in one transaction. Charging
// before forwarding is what makes the quota safe under concurrency.
func (s *Store) Reserve(licenseID, hwid string, cost Money, provider, model, poolKeyID string) (*Reservation, error) {
	if cost < 0 {
		return nil, errors.New("cost cannot be negative")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var active int
	var boundHWID string
	err = tx.QueryRow(`SELECT active, hwid FROM licenses WHERE id = ?`, licenseID).
		Scan(&active, &boundHWID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if active == 0 {
		return nil, ErrInactive
	}
	if boundHWID != hwid {
		return nil, ErrHWIDMismatch
	}

	now := time.Now().UTC()
	// This WHERE clause is the enforcement: it refuses rather than overdraws.
	res, err := tx.Exec(
		`UPDATE licenses SET spent_cents = spent_cents + ?, last_seen_at = ?
		 WHERE id = ? AND active = 1 AND spent_cents + ? <= quota_cents`,
		int64(cost), iso(now), licenseID, int64(cost))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrExhausted
	}

	if poolKeyID != "" {
		if _, err := tx.Exec(
			`UPDATE pool_keys SET spent_cents = spent_cents + ?, last_used_at = ? WHERE id = ?`,
			int64(cost), iso(now), poolKeyID); err != nil {
			return nil, err
		}
		// Retire a pooled key that has just run dry so it is not handed out again.
		if _, err := tx.Exec(
			`UPDATE pool_keys SET active = 0, exhausted_at = ?
			 WHERE id = ? AND spent_cents >= balance_cents AND exhausted_at IS NULL`,
			iso(now), poolKeyID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Reservation{EventID: NewID(), LicenseID: licenseID, PoolKeyID: poolKeyID, Cost: cost}, nil
}

// Commit records the reservation as a served request.
func (s *Store) Commit(r *Reservation, provider, model, poolKeyID string, status int, streamed bool) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_events
		   (id, license_id, pool_key_id, provider, model, cost_cents, streamed, status_code, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.EventID, r.LicenseID, poolKeyID, provider, model,
		int64(r.Cost), boolToInt(streamed), status, iso(time.Now().UTC()))
	return err
}

// Release refunds a reservation when the request could not be billed.
func (s *Store) Release(r *Reservation) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE licenses SET spent_cents = MAX(0, spent_cents - ?) WHERE id = ?`,
		int64(r.Cost), r.LicenseID); err != nil {
		return err
	}
	if r.PoolKeyID != "" {
		if _, err := tx.Exec(
			`UPDATE pool_keys SET spent_cents = MAX(0, spent_cents - ?) WHERE id = ?`,
			int64(r.Cost), r.PoolKeyID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const licenseColumns = `id, key_hint, hwid, quota_cents, spent_cents, active, note, created_at, bound_at, last_seen_at, raw_key`

func (s *Store) Get(licenseID string) (*License, error) {
	return scanLicense(s.db.QueryRow(`SELECT `+licenseColumns+` FROM licenses WHERE id = ?`, licenseID))
}

func (s *Store) byKeyHash(hash string) (*License, error) {
	return scanLicense(s.db.QueryRow(`SELECT `+licenseColumns+` FROM licenses WHERE key_hash = ?`, hash))
}

func (s *Store) List() []*License {
	rows, err := s.db.Query(`SELECT ` + licenseColumns + ` FROM licenses ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*License
	for rows.Next() {
		l, err := scanLicense(rows)
		if err != nil {
			continue
		}
		out = append(out, l)
	}
	return out
}

func (s *Store) SetActive(licenseID string, active bool) error {
	return s.affectOne(`UPDATE licenses SET active = ? WHERE id = ?`, boolToInt(active), licenseID)
}

func (s *Store) DeleteLicense(licenseID string) error {
	return s.affectOne(`DELETE FROM licenses WHERE id = ?`, licenseID)
}

// ResetHWID unbinds a licence so it can move to a new machine.
func (s *Store) ResetHWID(licenseID string) error {
	return s.affectOne(`UPDATE licenses SET hwid = '', bound_at = NULL WHERE id = ?`, licenseID)
}

func (s *Store) SetQuota(licenseID string, quota Money) error {
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	return s.affectOne(`UPDATE licenses SET quota_cents = ? WHERE id = ?`, int64(quota), licenseID)
}

func normalizePoolGroup(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return "default"
	}
	return group
}

// AddPoolKey stores an upstream credential, encrypted, along with the dollar
// balance loaded onto it so the relay can rotate away before it runs dry.
func (s *Store) AddPoolKey(label, secret, provider string, balance Money) (*PoolKey, error) {
	return s.AddPoolKeyInGroup(label, secret, provider, "default", balance)
}

func (s *Store) AddPoolKeyInGroup(label, secret, provider, poolGroup string, balance Money) (*PoolKey, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("pool key secret is required")
	}
	if provider != ProviderClaude {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	poolGroup = normalizePoolGroup(poolGroup)
	if balance <= 0 {
		return nil, errors.New("pool key balance must be positive")
	}
	enc, err := s.encrypt(secret)
	if err != nil {
		return nil, err
	}
	k := &PoolKey{
		ID:           NewID(),
		Label:        label,
		Provider:     provider,
		PoolGroup:    poolGroup,
		Secret:       secret,
		BalanceCents: balance,
		Active:       true,
		CreatedAt:    time.Now().UTC(),
	}
	if _, err := s.db.Exec(
		`INSERT INTO pool_keys (id, label, provider, pool_group, secret_enc, balance_cents, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.ID, label, provider, poolGroup, enc, int64(balance), iso(k.CreatedAt)); err != nil {
		return nil, err
	}
	return k, nil
}

// TakePoolKey returns the active key for a provider with the most credit left,
// so spend spreads evenly and a nearly empty key is retired rather than used.
func (s *Store) TakePoolKey(provider string) (*PoolKey, error) {
	return s.TakePoolKeyForGroup(provider, "default")
}

func (s *Store) TakePoolKeyForGroup(provider, poolGroup string) (*PoolKey, error) {
	poolGroup = normalizePoolGroup(poolGroup)
	row := s.db.QueryRow(
		`SELECT id, label, provider, pool_group, secret_enc, balance_cents, spent_cents, active, created_at, last_used_at
		   FROM pool_keys
		  WHERE provider = ? AND pool_group = ? AND active = 1 AND spent_cents < balance_cents
		  ORDER BY (balance_cents - spent_cents) DESC
		  LIMIT 1`, provider, poolGroup)

	var (
		k              PoolKey
		enc            []byte
		active         int
		created        string
		lastUsed       sql.NullString
		balance, spent int64
	)
	err := row.Scan(&k.ID, &k.Label, &k.Provider, &k.PoolGroup, &enc, &balance, &spent, &active, &created, &lastUsed)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no pooled key with credit for provider %q in pool group %q", provider, poolGroup)
	}
	if err != nil {
		return nil, err
	}

	secret, err := s.decrypt(enc)
	if err != nil {
		return nil, err
	}
	k.Secret = secret
	k.BalanceCents = Money(balance)
	k.SpentCents = Money(spent)
	k.Active = active == 1
	k.CreatedAt = parseISO(created)
	k.LastUsed = parseISOPtr(lastUsed)
	return &k, nil
}

// ListPoolKeys returns the pool with secrets removed.
func (s *Store) ListPoolKeys() []*PoolKey {
	return s.ListPoolKeysByGroup("")
}

func (s *Store) ListPoolKeysByGroup(poolGroup string) []*PoolKey {
	poolGroup = strings.TrimSpace(poolGroup)
	query := `SELECT id, label, provider, pool_group, balance_cents, spent_cents, active, created_at, last_used_at, exhausted_at
		   FROM pool_keys`
	args := []any{}
	if poolGroup != "" {
		query += ` WHERE pool_group = ?`
		args = append(args, poolGroup)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(
		query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*PoolKey
	for rows.Next() {
		var (
			k                   PoolKey
			active              int
			created             string
			lastUsed, exhausted sql.NullString
			balance, spent      int64
		)
		if err := rows.Scan(&k.ID, &k.Label, &k.Provider, &k.PoolGroup, &balance, &spent, &active,
			&created, &lastUsed, &exhausted); err != nil {
			continue
		}
		k.BalanceCents = Money(balance)
		k.SpentCents = Money(spent)
		k.Active = active == 1
		k.CreatedAt = parseISO(created)
		k.LastUsed = parseISOPtr(lastUsed)
		k.ExhaustedAt = parseISOPtr(exhausted)
		out = append(out, &k)
	}
	return out
}

func (s *Store) GetPoolKey(id string) (*PoolKey, error) {
	row := s.db.QueryRow(
		`SELECT id, label, provider, pool_group, balance_cents, spent_cents, active, created_at, last_used_at, exhausted_at
		   FROM pool_keys WHERE id = ?`, id)
	var (
		k                   PoolKey
		active              int
		created             string
		lastUsed, exhausted sql.NullString
		balance, spent      int64
	)
	if err := row.Scan(&k.ID, &k.Label, &k.Provider, &k.PoolGroup, &balance, &spent, &active, &created, &lastUsed, &exhausted); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	k.BalanceCents = Money(balance)
	k.SpentCents = Money(spent)
	k.Active = active == 1
	k.CreatedAt = parseISO(created)
	k.LastUsed = parseISOPtr(lastUsed)
	k.ExhaustedAt = parseISOPtr(exhausted)
	return &k, nil
}

func (s *Store) TopUpPoolKey(id string, amount Money) error {
	if amount <= 0 {
		return errors.New("top-up amount must be positive")
	}
	return s.affectOne(`UPDATE pool_keys SET balance_cents = balance_cents + ?, exhausted_at = NULL WHERE id = ?`, int64(amount), id)
}

func (s *Store) RotatePoolKey(id, label, secret string, balance Money) (*PoolKey, error) {
	oldKey, err := s.GetPoolKey(id)
	if err != nil {
		return nil, err
	}
	newLabel := strings.TrimSpace(label)
	if newLabel == "" {
		newLabel = oldKey.Label
	}
	newKey, err := s.AddPoolKeyInGroup(newLabel, secret, oldKey.Provider, oldKey.PoolGroup, balance)
	if err != nil {
		return nil, err
	}
	if err := s.SetPoolKeyActive(id, false); err != nil {
		return nil, err
	}
	return newKey, nil
}

func (s *Store) SetPoolKeyActive(id string, active bool) error {
	if !active {
		return s.affectOne(`UPDATE pool_keys SET active = 0 WHERE id = ?`, id)
	}
	key, err := s.GetPoolKey(id)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE pool_keys SET active = 0 WHERE provider = ? AND pool_group = ?`, key.Provider, key.PoolGroup); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE pool_keys SET active = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) RemovePoolKey(id string) error {
	return s.affectOne(`DELETE FROM pool_keys WHERE id = ?`, id)
}

func (s *Store) SaveEndpointProfile(name, claudeBaseURL, poolGroup string, active bool) (*EndpointProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("endpoint profile name is required")
	}
	claudeBaseURL = strings.TrimRight(strings.TrimSpace(claudeBaseURL), "/")
	if claudeBaseURL == "" {
		return nil, errors.New("claude base url is required")
	}
	poolGroup = normalizePoolGroup(poolGroup)
	createdAt := iso(time.Now().UTC())
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if active {
		if _, err := tx.Exec(`UPDATE endpoint_profiles SET active = 0`); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO endpoint_profiles (name, claude_base_url, pool_group, active, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		 claude_base_url = excluded.claude_base_url,
		 pool_group = excluded.pool_group,
		 active = excluded.active`,
		name, claudeBaseURL, poolGroup, boolToInt(active), createdAt,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetEndpointProfile(name)
}

func (s *Store) GetEndpointProfile(name string) (*EndpointProfile, error) {
	row := s.db.QueryRow(`SELECT name, claude_base_url, pool_group, active, created_at FROM endpoint_profiles WHERE name = ?`, strings.TrimSpace(name))
	var (
		ep      EndpointProfile
		active  int
		created string
	)
	if err := row.Scan(&ep.Name, &ep.ClaudeBaseURL, &ep.PoolGroup, &active, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ep.Active = active == 1
	ep.CreatedAt = parseISO(created)
	return &ep, nil
}

func (s *Store) ListEndpointProfiles() []*EndpointProfile {
	rows, err := s.db.Query(`SELECT name, claude_base_url, pool_group, active, created_at FROM endpoint_profiles ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []*EndpointProfile{}
	for rows.Next() {
		var (
			ep      EndpointProfile
			active  int
			created string
		)
		if err := rows.Scan(&ep.Name, &ep.ClaudeBaseURL, &ep.PoolGroup, &active, &created); err != nil {
			continue
		}
		ep.Active = active == 1
		ep.CreatedAt = parseISO(created)
		out = append(out, &ep)
	}
	return out
}

func (s *Store) SetActiveEndpointProfile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE endpoint_profiles SET active = 0`); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE endpoint_profiles SET active = 1 WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) DeleteEndpointProfile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNotFound
	}
	ep, err := s.GetEndpointProfile(name)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM endpoint_profiles WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM pool_keys WHERE pool_group = ?`, ep.PoolGroup); err != nil {
		return err
	}
	if ep.Active {
		if _, err := tx.Exec(`UPDATE endpoint_profiles SET active = 1 WHERE name = (SELECT name FROM endpoint_profiles ORDER BY created_at DESC LIMIT 1)`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ActiveEndpointProfile() (*EndpointProfile, error) {
	row := s.db.QueryRow(`SELECT name, claude_base_url, pool_group, active, created_at FROM endpoint_profiles WHERE active = 1 ORDER BY created_at DESC LIMIT 1`)
	var (
		ep      EndpointProfile
		active  int
		created string
	)
	if err := row.Scan(&ep.Name, &ep.ClaudeBaseURL, &ep.PoolGroup, &active, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ep.Active = active == 1
	ep.CreatedAt = parseISO(created)
	return &ep, nil
}

// Usage returns recorded requests, newest first, optionally for one licence.
func (s *Store) Usage(licenseID string, limit int) []*UsageEvent {
	return s.UsageFiltered(licenseID, "", "", limit)
}

func (s *Store) UsageFiltered(licenseID, search, status string, limit int) []*UsageEvent {
	if limit <= 0 {
		limit = 500
	}
	query := `SELECT id, license_id, pool_key_id, provider, model, cost_cents, streamed, status_code, created_at
	            FROM usage_events`
	args := []any{}
	clauses := []string{}
	if licenseID != "" {
		clauses = append(clauses, `license_id = ?`)
		args = append(args, licenseID)
	}
	if search != "" {
		clauses = append(clauses, `(provider LIKE ? OR model LIKE ? OR license_id LIKE ? OR pool_key_id LIKE ?)`)
		like := "%" + search + "%"
		args = append(args, like, like, like, like)
	}
	if status != "" {
		switch status {
		case "success":
			clauses = append(clauses, `status_code >= 200 AND status_code < 400`)
		case "error":
			clauses = append(clauses, `status_code >= 400`)
		case "streamed":
			clauses = append(clauses, `streamed = 1`)
		case "non-streamed":
			clauses = append(clauses, `streamed = 0`)
		}
	}
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, ` AND `)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*UsageEvent
	for rows.Next() {
		var (
			e        UsageEvent
			cost     int64
			streamed int
			created  string
		)
		if err := rows.Scan(&e.ID, &e.LicenseID, &e.PoolKeyID, &e.Provider, &e.Model,
			&cost, &streamed, &e.StatusCode, &created); err != nil {
			continue
		}
		e.CostCents = Money(cost)
		e.Streamed = streamed == 1
		e.CreatedAt = parseISO(created)
		out = append(out, &e)
	}
	return out
}

func (s *Store) affectOne(query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) encrypt(plain string) ([]byte, error) {
	gcm, err := s.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (s *Store) decrypt(enc []byte) (string, error) {
	gcm, err := s.gcm()
	if err != nil {
		return "", err
	}
	if len(enc) < gcm.NonceSize() {
		return "", errors.New("license: stored secret is corrupt")
	}
	nonce, body := enc[:gcm.NonceSize()], enc[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("license: cannot decrypt pooled secret (wrong RELAY_DB_KEY?): %w", err)
	}
	return string(plain), nil
}

func (s *Store) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.encKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLicense(row scanner) (*License, error) {
	var (
		l               License
		active          int
		created         string
		bound, lastSeen sql.NullString
		quota, spent    int64
	)
	err := row.Scan(&l.ID, &l.KeyHint, &l.HWID, &quota, &spent, &active, &l.Note,
		&created, &bound, &lastSeen, &l.RawKey)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	l.QuotaCents = Money(quota)
	l.SpentCents = Money(spent)
	l.Active = active == 1
	l.CreatedAt = parseISO(created)
	l.BoundAt = parseISOPtr(bound)
	l.LastSeenAt = parseISOPtr(lastSeen)
	return &l, nil
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(NormalizeKey(key)))
	return hex.EncodeToString(sum[:])
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseISO(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseISOPtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseISO(s.String)
	return &t
}
