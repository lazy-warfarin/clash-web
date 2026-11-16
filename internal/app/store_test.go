package app

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStoreProfileLifecycle(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProfile(Profile{Name: "demo", Source: "local", Content: "proxies: []"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ActivateProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Profile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Active || got.Name != "demo" {
		t.Fatalf("unexpected profile: %+v", got)
	}
	if err = s.DeleteProfile(p.ID); err != nil {
		t.Fatal(err)
	}
}

// TestStoreMigratesLegacySchema builds a pre-settings database by hand and
// verifies OpenStore adds the new columns without losing existing rows.
func TestStoreMigratesLegacySchema(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE profiles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, source TEXT NOT NULL, url TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 0, content TEXT NOT NULL, updated_at INTEGER NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO profiles(name,source,url,active,content,updated_at) VALUES('legacy','remote','http://example.com/sub',1,'proxies: []',1)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	profiles, err := s.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 migrated profile, got %d", len(profiles))
	}
	p := profiles[0]
	if p.Name != "legacy" || p.URL != "http://example.com/sub" || !p.Active {
		t.Fatalf("legacy row damaged by migration: %+v", p)
	}
	if p.Settings == nil || p.Settings.UpdateInterval != 0 || p.Settings.UserAgent != "" || p.Settings.WithProxy {
		t.Fatalf("unexpected migrated settings: %+v", p.Settings)
	}
	// New columns must be writable after migration.
	p.Settings.UpdateInterval = 1440
	p.Settings.UserAgent = "custom-agent"
	p.Settings.TimeoutSeconds = 60
	p.Settings.WithProxy = true
	if err = s.UpdateProfile(p); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.Profile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Settings.UpdateInterval != 1440 || reloaded.Settings.UserAgent != "custom-agent" || reloaded.Settings.TimeoutSeconds != 60 || !reloaded.Settings.WithProxy {
		t.Fatalf("settings not persisted after migration: %+v", reloaded.Settings)
	}
}

func TestProfilesDue(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	remote := func(name string, interval int) Profile {
		p, err := s.CreateProfile(Profile{Name: name, Source: "remote", URL: "http://example.com/" + name, Content: "proxies: []", Settings: &ProfileSettings{UpdateInterval: interval}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	never := remote("never", 0)
	hourly := remote("hourly", 60)
	daily := remote("daily", 1440)
	local, err := s.CreateProfile(Profile{Name: "local", Source: "local", Content: "proxies: []", Settings: &ProfileSettings{UpdateInterval: 60}})
	if err != nil {
		t.Fatal(err)
	}

	due, err := s.ProfilesDue(now)
	if err != nil {
		t.Fatal(err)
	}
	dueNames := map[string]bool{}
	for _, p := range due {
		dueNames[p.Name] = true
	}
	// Nothing is due immediately: created moments ago.
	if len(due) != 0 {
		t.Fatalf("fresh profiles reported due: %v", dueNames)
	}
	// Local profiles never refresh even with an interval set.
	if dueNames[local.Name] {
		t.Fatal("local profile reported due")
	}

	// Simulate elapsed time by rewinding updated_at.
	rewind := func(id int64, age time.Duration) {
		if _, err = s.db.Exec("UPDATE profiles SET updated_at=? WHERE id=?", now.Add(-age).Unix(), id); err != nil {
			t.Fatal(err)
		}
	}
	rewind(hourly.ID, 30*time.Minute)
	rewind(daily.ID, 25*time.Hour)
	rewind(local.ID, 48*time.Hour)
	due, err = s.ProfilesDue(now)
	if err != nil {
		t.Fatal(err)
	}
	dueNames = map[string]bool{}
	for _, p := range due {
		dueNames[p.Name] = true
	}
	// never/local are excluded; hourly (30m of 60m elapsed) is NOT yet due;
	// daily (25h of 24h elapsed) IS due.
	if dueNames[never.Name] {
		t.Fatal("interval=0 profile reported due")
	}
	if dueNames[hourly.Name] {
		t.Fatal("half-elapsed interval must not be due")
	}
	if !dueNames[daily.Name] {
		t.Fatal("elapsed daily interval should be due")
	}
	if dueNames[local.Name] {
		t.Fatal("local profile must never be due")
	}
	if len(due) != 1 {
		t.Fatalf("expected exactly 1 due profile, got %d: %v", len(due), dueNames)
	}
}

// TestSetProfileUpdateResult covers the failure semantics: updated_at always
// advances so a failed fetch is not retried on the very next tick.
func TestSetProfileUpdateResult(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProfile(Profile{Name: "remote", Source: "remote", URL: "http://example.com/sub", Content: "proxies: []", Settings: &ProfileSettings{UpdateInterval: 1440}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Profile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // updated_at has second resolution
	if err = s.SetProfileUpdateResult(p.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Profile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Settings.LastUpdateError != "boom" {
		t.Fatalf("error not recorded: %q", after.Settings.LastUpdateError)
	}
	if after.UpdatedAt <= before.UpdatedAt {
		t.Fatalf("updated_at must advance on failure: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
	due, err := s.ProfilesDue(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range due {
		if d.ID == p.ID {
			t.Fatal("failed profile is immediately due again; retry storm")
		}
	}
	if err = s.SetProfileUpdateResult(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.Profile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Settings.LastUpdateError != "" {
		t.Fatalf("error not cleared on success: %q", cleared.Settings.LastUpdateError)
	}
}

// TestUpdateProfileKeepsSettingsWithoutPayload verifies that a YAML-only edit
// (Settings == nil) leaves the stored subscription options untouched.
func TestUpdateProfileKeepsSettingsWithoutPayload(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProfile(Profile{Name: "remote", Source: "remote", URL: "http://example.com/sub", Content: "proxies: []", Settings: &ProfileSettings{UpdateInterval: 1440, UserAgent: "ua", TimeoutSeconds: 30, WithProxy: true}})
	if err != nil {
		t.Fatal(err)
	}
	p.Settings = nil
	if err = s.UpdateProfile(p); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.Profile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Settings == nil || reloaded.Settings.UpdateInterval != 1440 || reloaded.Settings.UserAgent != "ua" || !reloaded.Settings.WithProxy {
		t.Fatalf("nil settings payload wiped stored options: %+v", reloaded.Settings)
	}
}

// TestNormalizeSettings covers the validation clamps for profile settings.
func TestNormalizeSettings(t *testing.T) {
	raised := normalizeSettings(&ProfileSettings{UpdateInterval: 60})
	if raised.UpdateInterval != minAutoUpdateMinutes {
		t.Fatalf("interval below floor not raised: %d", raised.UpdateInterval)
	}
	kept := normalizeSettings(&ProfileSettings{UpdateInterval: 1440})
	if kept.UpdateInterval != 1440 {
		t.Fatalf("legal interval modified: %d", kept.UpdateInterval)
	}
	off := normalizeSettings(&ProfileSettings{UpdateInterval: -5})
	if off.UpdateInterval != 0 {
		t.Fatalf("negative interval not zeroed: %d", off.UpdateInterval)
	}
	capped := normalizeSettings(&ProfileSettings{TimeoutSeconds: 999})
	if capped.TimeoutSeconds != maxFetchTimeoutSeconds {
		t.Fatalf("timeout not capped: %d", capped.TimeoutSeconds)
	}
	stripped := normalizeSettings(&ProfileSettings{UserAgent: "ok-agent\r\n"})
	if stripped.UserAgent != "ok-agent" {
		t.Fatalf("control characters not stripped: %q", stripped.UserAgent)
	}
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	truncated := normalizeSettings(&ProfileSettings{UserAgent: string(long)})
	if len(truncated.UserAgent) != 512 {
		t.Fatalf("user agent not truncated: %d", len(truncated.UserAgent))
	}
}
