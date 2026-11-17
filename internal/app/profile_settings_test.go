package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Use a real session and router so the dedicated settings route is exercised
// with the same authentication/CSRF middleware as the application.
func settingsTestServer(t *testing.T) (*Server, func(string, string) *httptest.ResponseRecorder) {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err = store.EnsureAdmin("unused-test-hash"); err != nil {
		t.Fatal(err)
	}
	id, _, _, err := store.User("admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ChangePassword(id, "unused-test-hash", false); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(id, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store, cfg: Config{AllowPrivateSubscriptionSources: true}}
	handler := server.routes()
	request := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
		req.Host = "example.com"
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", "test-csrf")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "test-csrf"})
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	return server, request
}

func TestStoreProfileSettingsIndependentAndPersistent(t *testing.T) {
	directory := t.TempDir()
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	first, err := store.CreateProfile(Profile{Name: "first", Source: "remote", URL: "https://example.com/first", Content: "proxies: []", Settings: &ProfileSettings{UpdateInterval: 1440}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateProfile(Profile{Name: "second", Source: "remote", URL: "https://example.com/second", Content: "mode: direct", Settings: &ProfileSettings{UpdateInterval: 4320, UserAgent: "second-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ActivateProfile(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec("UPDATE profiles SET updated_at=1000,last_update_error='previous failure' WHERE id=?", first.ID); err != nil {
		t.Fatal(err)
	}
	before, err := store.Profile(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondBefore, err := store.Profile(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	wanted := ProfileSettings{UpdateInterval: 2880, UserAgent: "first-agent", TimeoutSeconds: 45, WithProxy: true}
	if err = store.UpdateProfileSettings(first.ID, wanted); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.Profile(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	wanted.LastUpdateError = "previous failure"
	if !reflect.DeepEqual(after.Settings, &wanted) {
		t.Fatalf("settings did not survive reopening: %+v", after.Settings)
	}
	before.Settings = after.Settings
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("saving settings changed YAML/metadata: before=%+v after=%+v", before, after)
	}
	secondAfter, err := store.Profile(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(secondBefore, secondAfter) {
		t.Fatal("saving first subscription changed the second subscription")
	}
	if err = store.UpdateProfileSettings(9999, wanted); err != sql.ErrNoRows {
		t.Fatalf("unknown profile error = %v, want sql.ErrNoRows", err)
	}
}

func TestProfileSettingsPatch(t *testing.T) {
	server, patch := settingsTestServer(t)
	profile, err := server.store.CreateProfile(Profile{Name: "subscription", Source: "remote", URL: "https://example.com/sub", Content: "proxies: []", Settings: &ProfileSettings{TimeoutSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/profiles/%d/settings", profile.ID)
	result := patch(path, `{"updateInterval":60,"userAgent":"custom-agent","timeoutSeconds":45,"withProxy":true}`)
	if result.Code != http.StatusOK {
		t.Fatalf("save returned %d: %s", result.Code, result.Body.String())
	}
	var saved ProfileSettings
	if err = json.Unmarshal(result.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.UpdateInterval != 1440 || saved.UserAgent != "custom-agent" || saved.TimeoutSeconds != 45 || !saved.WithProxy {
		t.Fatalf("unexpected saved settings: %+v", saved)
	}
	if strings.Contains(result.Body.String(), "proxies:") || strings.Contains(result.Body.String(), "example.com/sub") {
		t.Fatal("settings response unnecessarily included the subscription YAML or URL")
	}
	// An explicit zero switches scheduled updates off; omitted options survive.
	result = patch(path, `{"updateInterval":0}`)
	if result.Code != http.StatusOK {
		t.Fatalf("disable returned %d: %s", result.Code, result.Body.String())
	}
	profile, err = server.store.Profile(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Settings.UpdateInterval != 0 || profile.Settings.TimeoutSeconds != 45 || !profile.Settings.WithProxy || profile.Content != "proxies: []" {
		t.Fatalf("partial settings save changed unrelated data: %+v", profile)
	}
	result = patch(path, `{"updateInterval":2880}`)
	if result.Code != http.StatusOK {
		t.Fatalf("enable returned %d: %s", result.Code, result.Body.String())
	}
	profile, err = server.store.Profile(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Settings.UpdateInterval != 2880 {
		t.Fatal("re-enabling did not save the new interval")
	}
}

func TestProfileSettingsRejectInvalidRequests(t *testing.T) {
	server, patch := settingsTestServer(t)
	remote, err := server.store.CreateProfile(Profile{Name: "remote", Source: "remote", URL: "https://example.com/sub", Content: "mode: rule"})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/profiles/%d/settings", remote.ID)
	for _, body := range []string{`{}`, `null`, `{"updateInterval":-1}`, `{"updateInterval":1.5}`, `{"updateInterval":999999999999}`, `{"updateInterval":"1440"}`, `{"lastUpdateError":"fake"}`, `{"content":"overwritten"}`} {
		t.Run(body, func(t *testing.T) {
			result := patch(path, body)
			if result.Code != http.StatusBadRequest {
				t.Fatalf("got %d for invalid settings: %s", result.Code, result.Body.String())
			}
		})
	}
	local, err := server.store.CreateProfile(Profile{Name: "local", Source: "local", Content: "mode: direct"})
	if err != nil {
		t.Fatal(err)
	}
	if result := patch(fmt.Sprintf("/api/v1/profiles/%d/settings", local.ID), `{"updateInterval":1440}`); result.Code != http.StatusBadRequest {
		t.Fatalf("scheduled download allowed for local YAML: %d", result.Code)
	}
	if result := patch("/api/v1/profiles/9999/settings", `{"updateInterval":1440}`); result.Code != http.StatusNotFound {
		t.Fatalf("unknown profile returned %d", result.Code)
	}
	handler := server.routes()
	for _, test := range []struct {
		name string
		auth bool
		want int
	}{
		{"not logged in", false, http.StatusUnauthorized},
		{"missing csrf", true, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"updateInterval":1440}`))
			if test.auth {
				id, _, _, err := server.store.User("admin")
				if err != nil {
					t.Fatal(err)
				}
				token, err := server.store.CreateSession(id, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
			}
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, req)
			if result.Code != test.want {
				t.Fatalf("got %d, want %d", result.Code, test.want)
			}
		})
	}
}

func TestSubscriptionScheduleUsesPerProfileSettings(t *testing.T) {
	server, patch := settingsTestServer(t)
	var firstCalls, secondCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			firstCalls.Add(1)
			if r.UserAgent() != "first-agent" {
				t.Errorf("scheduled request used User-Agent %q", r.UserAgent())
			}
		} else {
			secondCalls.Add(1)
		}
		_, _ = w.Write([]byte("mode: rule\nproxies: []\n"))
	}))
	defer upstream.Close()
	first, err := server.store.CreateProfile(Profile{Name: "first", Source: "remote", URL: upstream.URL + "/first", Content: "mode: direct", Settings: &ProfileSettings{UpdateInterval: 1440}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.store.CreateProfile(Profile{Name: "second", Source: "remote", URL: upstream.URL + "/second", Content: "mode: direct", Settings: &ProfileSettings{UpdateInterval: 1440}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err = server.store.db.Exec("UPDATE profiles SET updated_at=?", now.Add(-72*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/profiles/%d/settings", first.ID)
	if result := patch(path, `{"updateInterval":0}`); result.Code != http.StatusOK {
		t.Fatalf("disable failed: %s", result.Body.String())
	}
	if err = server.refreshDueProfiles(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if firstCalls.Load() != 0 || secondCalls.Load() != 1 {
		t.Fatalf("disabled subscription ran or affected another subscription: first=%d second=%d", firstCalls.Load(), secondCalls.Load())
	}
	if result := patch(path, `{"updateInterval":2880,"userAgent":"first-agent"}`); result.Code != http.StatusOK {
		t.Fatalf("enable failed: %s", result.Body.String())
	}
	// The older last-refresh time is preserved, so the newly enabled schedule
	// is already due. The second subscription was just refreshed and is not due.
	if err = server.refreshDueProfiles(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if firstCalls.Load() != 1 || secondCalls.Load() != 1 {
		t.Fatalf("independent schedules incorrect: first=%d second=%d (id=%d)", firstCalls.Load(), secondCalls.Load(), second.ID)
	}
	// A queued scheduler must observe a disable that was saved while waiting
	// for the refresh lock, not a previously cached due-list snapshot.
	if _, err = server.store.db.Exec("UPDATE profiles SET updated_at=? WHERE id=?", now.Add(-72*time.Hour).Unix(), first.ID); err != nil {
		t.Fatal(err)
	}
	server.refreshMu.Lock()
	done := make(chan error, 1)
	go func() { done <- server.refreshDueProfiles(context.Background(), time.Now()) }()
	err = server.store.UpdateProfileSettings(first.ID, ProfileSettings{UpdateInterval: 0})
	server.refreshMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not release the refresh lock")
	}
	if firstCalls.Load() != 1 {
		t.Fatal("queued scheduler fetched a subscription after its schedule was disabled")
	}
}
