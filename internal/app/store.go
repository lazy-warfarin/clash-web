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

type Profile struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source"`
	URL       string `json:"url,omitempty"`
	Active    bool   `json:"active"`
	UpdatedAt string `json:"updatedAt"`
	Content   string `json:"content,omitempty"`
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
CREATE TABLE IF NOT EXISTS profiles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, source TEXT NOT NULL, url TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 0, content TEXT NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
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

func (s *Store) Profiles() ([]Profile, error) {
	rows, err := s.db.Query("SELECT id,name,source,url,active,datetime(updated_at,'unixepoch') FROM profiles ORDER BY active DESC,id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Profile, 0)
	for rows.Next() {
		var p Profile
		var active int
		if err := rows.Scan(&p.ID, &p.Name, &p.Source, &p.URL, &active, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Active = active != 0
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) Profile(id int64) (Profile, error) {
	var p Profile
	var active int
	var ts int64
	err := s.db.QueryRow("SELECT id,name,source,url,active,content,updated_at FROM profiles WHERE id=?", id).Scan(&p.ID, &p.Name, &p.Source, &p.URL, &active, &p.Content, &ts)
	p.Active = active != 0
	p.UpdatedAt = time.Unix(ts, 0).UTC().Format(time.RFC3339)
	return p, err
}

func (s *Store) CreateProfile(p Profile) (Profile, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec("INSERT INTO profiles(name,source,url,content,updated_at) VALUES(?,?,?,?,?)", p.Name, p.Source, p.URL, p.Content, now)
	if err != nil {
		return p, err
	}
	p.ID, _ = res.LastInsertId()
	p.UpdatedAt = time.Unix(now, 0).UTC().Format(time.RFC3339)
	return p, nil
}

func (s *Store) UpdateProfile(p Profile) error {
	_, err := s.db.Exec("UPDATE profiles SET name=?,url=?,content=?,updated_at=? WHERE id=?", p.Name, p.URL, p.Content, time.Now().Unix(), p.ID)
	return err
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
