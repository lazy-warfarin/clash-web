package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAuthenticationCSRFAndForcedPasswordChange(t *testing.T) {
	t.Setenv("CLASH_WEB_ADMIN_PASSWORD", "initial-password-123")
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.RuntimeDir = t.TempDir()
	cfg.MihomoController = "http://127.0.0.1:1"
	cfg.HelperSocket = "tcp://127.0.0.1:2"
	server, err := New(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	handler := server.routes()

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"initial-password-123"}`))
	login.Header.Set("Content-Type", "application/json")
	login.RemoteAddr = "192.0.2.10:1234"
	loginResult := httptest.NewRecorder()
	handler.ServeHTTP(loginResult, login)
	if loginResult.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", loginResult.Code, loginResult.Body.String())
	}

	var session, csrf *http.Cookie
	for _, cookie := range loginResult.Result().Cookies() {
		switch cookie.Name {
		case sessionCookie:
			session = cookie
		case csrfCookie:
			csrf = cookie
		}
	}
	if session == nil || csrf == nil {
		t.Fatal("authentication cookies were not issued")
	}

	status := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	status.AddCookie(session)
	statusResult := httptest.NewRecorder()
	handler.ServeHTTP(statusResult, status)
	if statusResult.Code != http.StatusPreconditionRequired {
		t.Fatalf("expected forced password change, got %d", statusResult.Code)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(session)
	logoutResult := httptest.NewRecorder()
	handler.ServeHTTP(logoutResult, logout)
	if logoutResult.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF token returned %d", logoutResult.Code)
	}

	logout = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.Host = "example.com"
	logout.Header.Set("Origin", "http://example.com")
	logout.Header.Set("X-CSRF-Token", csrf.Value)
	logout.AddCookie(session)
	logout.AddCookie(csrf)
	logoutResult = httptest.NewRecorder()
	handler.ServeHTTP(logoutResult, logout)
	if logoutResult.Code != http.StatusNoContent {
		t.Fatalf("valid logout returned %d: %s", logoutResult.Code, logoutResult.Body.String())
	}
}

func TestProxyDynamicResolvesEachRequestIndependently(t *testing.T) {
	paths := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	server := &Server{mihomo: upstream.Client(), mihomoBase: upstream.URL}
	router := chi.NewRouter()
	router.Put("/proxies/{name}", server.proxyDynamic(http.MethodPut, "/proxies/{name}"))

	for _, name := range []string{"一分机场", "自动选择"} {
		req := httptest.NewRequest(http.MethodPut, "/proxies/"+url.PathEscape(name), strings.NewReader(`{"name":"node"}`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("request for %q returned %d", name, res.Code)
		}
	}

	for _, want := range []string{"/proxies/%E4%B8%80%E5%88%86%E6%9C%BA%E5%9C%BA", "/proxies/%E8%87%AA%E5%8A%A8%E9%80%89%E6%8B%A9"} {
		if got := <-paths; got != want {
			t.Fatalf("upstream path = %q, want %q", got, want)
		}
	}
}
