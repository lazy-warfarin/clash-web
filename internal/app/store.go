package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

// ProfileSettings holds per-profile subscription options, mirroring the
// clash-verge-rev profile option fields that make sense for a server.
type ProfileSettings struct {
	UpdateInterval  int    `json:"updateInterval"`  // minutes; 0 disables scheduled updates
	UserAgent       string `json:"userAgent"`       // empty = default "Clash.Meta"
	TimeoutSeconds  int    `json:"timeoutSeconds"`  // 0 = default 25s
	WithProxy       bool   `json:"withProxy"`       // fetch through the local mihomo port
	LastUpdateError string `json:"lastUpdateError"` // outcome of the last refresh, cleared on success
}

type Profile struct {
	ID        int64            `json:"id"`
	Name      string           `json:"name"`
	Source    string           `json:"source"`
	URL       string           `json:"url,omitempty"`
	Active    bool             `json:"active"`
	UpdatedAt string           `json:"updatedAt"`
	Content   string           `json:"content,omitempty"`
	Settings  *ProfileSettings `json:"settings,omitempty"`
}

func OpenStore(dataDir string) (*Store, error) {
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "app.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	_, err = db.Exec(`
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, must_change INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at INTEGER NOT NULL, FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS profiles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, source TEXT NOT NULL, url TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 0, content TEXT NOT NULL, updated_at INTEGER NOT NULL, update_interval INTEGER NOT NULL DEFAULT 0, user_agent TEXT NOT NULL DEFAULT '', timeout_seconds INTEGER NOT NULL DEFAULT 0, with_proxy INTEGER NOT NULL DEFAULT 0, last_update_error TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err = s.migrateProfiles(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrateProfiles appends per-profile setting columns missing from databases
// created before they existed. CREATE TABLE IF NOT EXISTS leaves an existing
// table untouched, so this check runs on every open and is idempotent.
func (s *Store) migrateProfiles() error {
	rows, err := s.db.Query("PRAGMA table_info(profiles)")
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var dflt sql.NullString
		if err = rows.Scan(&cid, &name, &columnType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	additions := []struct{ column, definition string }{
		{"update_interval", "INTEGER NOT NULL DEFAULT 0"},
		{"user_agent", "TEXT NOT NULL DEFAULT ''"},
		{"timeout_seconds", "INTEGER NOT NULL DEFAULT 0"},
		{"with_proxy", "INTEGER NOT NULL DEFAULT 0"},
		{"last_update_error", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, addition := range additions {
		if existing[addition.column] {
			continue
		}
		if _, err = s.db.Exec("ALTER TABLE profiles ADD COLUMN " + addition.column + " " + addition.definition); err != nil {
			return fmt.Errorf("migrate profiles table: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) EnsureAdmin(hash string) (bool, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	_, err := s.db.Exec("INSERT INTO users(username,password_hash,must_change) VALUES('admin',?,1)", hash)
	return true, err
}

func (s *Store) User(username string) (id int64, hash string, mustChange bool, err error) {
	var must int
	err = s.db.QueryRow("SELECT id,password_hash,must_change FROM users WHERE username=?", username).Scan(&id, &hash, &must)
	mustChange = must != 0
	return
}

func (s *Store) ChangePassword(userID int64, hash string, mustChange bool) error {
	_, err := s.db.Exec("UPDATE users SET password_hash=?,must_change=? WHERE id=?", hash, boolInt(mustChange), userID)
	return err
}

func (s *Store) ReplaceAdminPassword(hash string) error {
	res, err := s.db.Exec("UPDATE users SET password_hash=?,must_change=1 WHERE username='admin'", hash)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_, err = s.db.Exec("INSERT INTO users(username,password_hash,must_change) VALUES('admin',?,1)", hash)
	}
	_, _ = s.db.Exec("DELETE FROM sessions")
	return err
}

func (s *Store) CreateSession(userID int64, ttl time.Duration) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", hashToken(token), userID, time.Now().Add(ttl).Unix())
	return token, err
}

func (s *Store) Session(token string) (int64, bool, error) {
	var id int64
	var must int
	err := s.db.QueryRow(`SELECT u.id,u.must_change FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, hashToken(token), time.Now().Unix()).Scan(&id, &must)
	return id, must != 0, err
}

func (s *Store) DeleteSession(token string) {
	_, _ = s.db.Exec("DELETE FROM sessions WHERE token_hash=?", hashToken(token))
}

const profileListColumns = "id,name,source,url,active,updated_at,update_interval,user_agent,timeout_seconds,with_proxy,last_update_error"

func (s *Store) Profiles() ([]Profile, error) {
	rows, err := s.db.Query("SELECT " + profileListColumns + " FROM profiles ORDER BY active DESC,id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Profile, 0)
	for rows.Next() {
		var p Profile
		var active, interval, timeout, withProxy int
		var ts int64
		settings := &ProfileSettings{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Source, &p.URL, &active, &ts, &interval, &settings.UserAgent, &timeout, &withProxy, &settings.LastUpdateError); err != nil {
			return nil, err
		}
		p.Active = active != 0
		p.UpdatedAt = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		settings.UpdateInterval = interval
		settings.TimeoutSeconds = timeout
		settings.WithProxy = withProxy != 0
		p.Settings = settings
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) Profile(id int64) (Profile, error) {
	var p Profile
	var active, interval, timeout, withProxy int
	var ts int64
	settings := &ProfileSettings{}
	err := s.db.QueryRow("SELECT id,name,source,url,active,content,updated_at,update_interval,user_agent,timeout_seconds,with_proxy,last_update_error FROM profiles WHERE id=?", id).
		Scan(&p.ID, &p.Name, &p.Source, &p.URL, &active, &p.Content, &ts, &interval, &settings.UserAgent, &timeout, &withProxy, &settings.LastUpdateError)
	p.Active = active != 0
	p.UpdatedAt = time.Unix(ts, 0).UTC().Format(time.RFC3339)
	settings.UpdateInterval = interval
	settings.TimeoutSeconds = timeout
	settings.WithProxy = withProxy != 0
	p.Settings = settings
	return p, err
}

func (s *Store) CreateProfile(p Profile) (Profile, error) {
	now := time.Now().Unix()
	settings := p.Settings
	if settings == nil {
		settings = &ProfileSettings{}
	}
	res, err := s.db.Exec("INSERT INTO profiles(name,source,url,content,updated_at,update_interval,user_agent,timeout_seconds,with_proxy,last_update_error) VALUES(?,?,?,?,?,?,?,?,?,?)",
		p.Name, p.Source, p.URL, p.Content, now, settings.UpdateInterval, settings.UserAgent, settings.TimeoutSeconds, boolInt(settings.WithProxy), settings.LastUpdateError)
	if err != nil {
		return p, err
	}
	p.ID, _ = res.LastInsertId()
	p.UpdatedAt = time.Unix(now, 0).UTC().Format(time.RFC3339)
	p.Settings = settings
	return p, nil
}

// UpdateProfile persists an edited profile. A nil Settings value leaves the
// stored subscription options untouched so YAML-only edits cannot wipe them.
func (s *Store) UpdateProfile(p Profile) error {
	if p.Settings == nil {
		_, err := s.db.Exec("UPDATE profiles SET name=?,url=?,content=?,updated_at=? WHERE id=?", p.Name, p.URL, p.Content, time.Now().Unix(), p.ID)
		return err
	}
	settings := p.Settings
	_, err := s.db.Exec("UPDATE profiles SET name=?,url=?,content=?,updated_at=?,update_interval=?,user_agent=?,timeout_seconds=?,with_proxy=? WHERE id=?",
		p.Name, p.URL, p.Content, time.Now().Unix(), settings.UpdateInterval, settings.UserAgent, settings.TimeoutSeconds, boolInt(settings.WithProxy), p.ID)
	return err
}

// UpdateProfileSettings changes only subscription options. Saving settings must
// not overwrite downloaded YAML, activation state, the last error or the last
// refresh time. Scheduling continues from that refresh time, even after restart.
func (s *Store) UpdateProfileSettings(id int64, settings ProfileSettings) error {
	result, err := s.db.Exec("UPDATE profiles SET update_interval=?,user_agent=?,timeout_seconds=?,with_proxy=? WHERE id=?",
		settings.UpdateInterval, settings.UserAgent, settings.TimeoutSeconds, boolInt(settings.WithProxy), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteProfile(id int64) error {
	_, err := s.db.Exec("DELETE FROM profiles WHERE id=?", id)
	return err
}

func (s *Store) ActivateProfile(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE profiles SET active=0"); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE profiles SET active=1 WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// ProfilesDue returns remote profiles whose scheduled update time has passed.
// See SetProfileUpdateResult for why failures also count as "updated".
func (s *Store) ProfilesDue(now time.Time) ([]Profile, error) {
	all, err := s.Profiles()
	if err != nil {
		return nil, err
	}
	due := make([]Profile, 0, len(all))
	for _, p := range all {
		if p.URL == "" || p.Settings == nil || p.Settings.UpdateInterval <= 0 {
			continue
		}
		updated, err := time.Parse(time.RFC3339, p.UpdatedAt)
		if err != nil {
			continue
		}
		if now.Sub(updated) >= time.Duration(p.Settings.UpdateInterval)*time.Minute {
			due = append(due, p)
		}
	}
	return due, nil
}

// SetProfileUpdateResult records the outcome of a refresh. updated_at always
// advances: a failed fetch must push the next attempt a full interval away,
// otherwise ProfilesDue would fire again on the next tick and spin.
func (s *Store) SetProfileUpdateResult(id int64, errText string) error {
	_, err := s.db.Exec("UPDATE profiles SET last_update_error=?,updated_at=? WHERE id=?", errText, time.Now().Unix(), id)
	return err
}

func (s *Store) Setting(key string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v)
	return v, err
}
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}

func randomToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func writeBootstrapPassword(dataDir, password string) error {
	return os.WriteFile(filepath.Join(dataDir, "bootstrap-password"), []byte(password+"\n"), 0o600)
}

func requireRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("not found")
	}
	return err
}
