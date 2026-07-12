package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/maris/clash-web/internal/webassets"
)

const (
	sessionCookie = "clash_web_session"
	csrfCookie    = "clash_web_csrf"
)

type Server struct {
	cfg           Config
	version       string
	store         *Store
	mihomo        *http.Client
	mihomoBase    string
	helper        *http.Client
	helperBase    string
	loginMu       sync.Mutex
	loginAttempts map[string][]time.Time
}

type contextKey string

const userKey contextKey = "user"

func New(cfg Config, version string) (*Server, error) {
	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	password := os.Getenv("CLASH_WEB_ADMIN_PASSWORD")
	if password == "" {
		password, err = generatePassword()
		if err != nil {
			return nil, err
		}
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	created, err := store.EnsureAdmin(hash)
	if err != nil {
		return nil, err
	}
	if created {
		if err := writeBootstrapPassword(cfg.DataDir, password); err != nil {
			return nil, err
		}
		log.Printf("first-run administrator password written to %s", filepath.Join(cfg.DataDir, "bootstrap-password"))
	}
	mClient, mBase, err := socketHTTPClient(cfg.MihomoController, 30*time.Second)
	if err != nil {
		return nil, err
	}
	hClient, hBase, err := socketHTTPClient(cfg.HelperSocket, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, version: version, store: store, mihomo: mClient, mihomoBase: mBase, helper: hClient, helperBase: hBase, loginAttempts: map[string][]time.Time{}}, nil
}

func (s *Server) Close() { _ = s.store.Close() }

func (s *Server) Run(ctx context.Context) error {
	httpServer := &http.Server{Addr: s.cfg.Listen, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("web console listening on http://%s", s.cfg.Listen)
		errCh <- httpServer.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, s.securityHeaders)
	r.Use(cors.Handler(cors.Options{AllowedOrigins: []string{"http://*", "https://*"}, AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}, AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"}, AllowCredentials: true, MaxAge: 300}))
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.login)
		r.Group(func(r chi.Router) {
			r.Use(s.authenticate, s.checkOrigin)
			r.Get("/auth/me", s.me)
			r.Post("/auth/logout", s.logout)
			r.Post("/auth/password", s.changePassword)
			r.Get("/status", s.status)
			r.Route("/core", func(r chi.Router) { r.Post("/{action}", s.coreAction) })
			r.Route("/profiles", func(r chi.Router) {
				r.Get("/", s.listProfiles)
				r.Post("/", s.createProfile)
				r.Get("/{id}", s.getProfile)
				r.Put("/{id}", s.updateProfile)
				r.Delete("/{id}", s.deleteProfile)
				r.Post("/{id}/activate", s.activateProfile)
				r.Post("/{id}/refresh", s.refreshProfile)
			})
			r.Get("/config", s.proxyMihomo("GET", "/configs"))
			r.Patch("/config", s.proxyHelper("PATCH", "/v1/config/overrides"))
			r.Get("/current-config", s.proxyHelper("GET", "/v1/config/current"))
			r.Put("/current-config", s.proxyHelper("PUT", "/v1/config/current"))
			r.Get("/cores", s.proxyHelper("GET", "/v1/cores"))
			r.Post("/cores/update", s.proxyHelper("POST", "/v1/cores/update"))
			r.Post("/cores/select", s.proxyHelper("POST", "/v1/cores/select"))
			r.Post("/geodata/update", s.proxyHelper("POST", "/v1/geodata/update"))
			r.Get("/proxies", s.proxyMihomo("GET", "/proxies"))
			r.Get("/proxies/{name}/delay", s.proxyDynamic("GET", "/proxies/{name}/delay"))
			r.Put("/proxies/{name}", s.proxyDynamic("PUT", "/proxies/{name}"))
			r.Get("/connections", s.proxyMihomo("GET", "/connections"))
			r.Delete("/connections", s.proxyMihomo("DELETE", "/connections"))
			r.Delete("/connections/{id}", s.proxyDynamic("DELETE", "/connections/{id}"))
			r.Get("/rules", s.proxyMihomo("GET", "/rules"))
			r.Get("/providers/proxies", s.proxyMihomo("GET", "/providers/proxies"))
			r.Put("/providers/proxies/{name}", s.proxyDynamic("PUT", "/providers/proxies/{name}"))
			r.Get("/providers/rules", s.proxyMihomo("GET", "/providers/rules"))
			r.Put("/providers/rules/{name}", s.proxyDynamic("PUT", "/providers/rules/{name}"))
			r.Get("/ws/{topic}", s.websocketProxy)
		})
	})
	r.Handle("/*", spaHandler())
	return r
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self' ws: wss:; font-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !sameHost(u.Host, r.Host) {
					writeError(w, http.StatusForbidden, "origin rejected")
					return
				}
			}
			cookie, err := r.Cookie(csrfCookie)
			if err != nil || cookie.Value == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
				writeError(w, http.StatusForbidden, "CSRF token rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.allowLogin(ip) {
		writeError(w, 429, "too many login attempts")
		return
	}
	var in struct{ Username, Password string }
	if !decodeJSON(w, r, &in, 64<<10) {
		return
	}
	id, hash, must, err := s.store.User(in.Username)
	if err != nil || !verifyPassword(hash, in.Password) {
		s.recordLogin(ip)
		writeError(w, 401, "invalid username or password")
		return
	}
	token, err := s.store.CreateSession(id, 24*time.Hour)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	secure := r.TLS != nil || (s.cfg.TrustedProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	csrf, err := randomToken(24)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: csrf, Path: "/", HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	writeJSON(w, 200, map[string]any{"username": in.Username, "mustChangePassword": must})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, 401, "authentication required")
			return
		}
		id, must, err := s.store.Session(cookie.Value)
		if err != nil {
			writeError(w, 401, "session expired")
			return
		}
		if must && r.URL.Path != "/api/v1/auth/me" && r.URL.Path != "/api/v1/auth/password" && r.URL.Path != "/api/v1/auth/logout" {
			writeError(w, http.StatusPreconditionRequired, "administrator password change required")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, struct {
			ID   int64
			Must bool
		}{id, must})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(struct {
		ID   int64
		Must bool
	})
	writeJSON(w, 200, map[string]any{"username": "admin", "mustChangePassword": u.Must, "version": s.version})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Path: "/", MaxAge: -1, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(204)
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Current, Password string }
	if !decodeJSON(w, r, &in, 64<<10) {
		return
	}
	u := r.Context().Value(userKey).(struct {
		ID   int64
		Must bool
	})
	_, hash, _, _ := s.store.User("admin")
	if !verifyPassword(hash, in.Current) {
		writeError(w, 403, "current password is incorrect")
		return
	}
	newHash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.store.ChangePassword(u.ID, newHash, false); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	result := map[string]any{"appVersion": s.version, "webListen": s.cfg.Listen, "coreOnline": false, "helperOnline": false}
	config := map[string]any{}
	if data, code, err := s.request(s.helper, "GET", s.helperBase+"/v1/status", nil, nil); err == nil && code < 400 {
		var helper any
		_ = json.Unmarshal(data, &helper)
		result["helperOnline"] = true
		result["helper"] = helper
	}
	if data, code, err := s.request(s.mihomo, "GET", s.mihomoBase+"/version", nil, nil); err == nil && code < 400 {
		var core any
		_ = json.Unmarshal(data, &core)
		result["coreOnline"] = true
		result["core"] = core
	}
	if data, code, err := s.request(s.mihomo, "GET", s.mihomoBase+"/configs", nil, nil); err == nil && code < 400 {
		_ = json.Unmarshal(data, &config)
	}
	if data, code, err := s.request(s.helper, "GET", s.helperBase+"/v1/config/settings", nil, nil); err == nil && code < 400 {
		var persisted map[string]any
		if json.Unmarshal(data, &persisted) == nil {
			mergeConfigMaps(config, persisted)
		}
	}
	result["config"] = config
	writeJSON(w, 200, result)
}

func mergeConfigMaps(target, source map[string]any) {
	for key, value := range source {
		if nestedSource, ok := value.(map[string]any); ok {
			if nestedTarget, ok := target[key].(map[string]any); ok {
				mergeConfigMaps(nestedTarget, nestedSource)
				continue
			}
		}
		target[key] = value
	}
}

func (s *Server) coreAction(w http.ResponseWriter, r *http.Request) {
	action := chi.URLParam(r, "action")
	if action != "start" && action != "stop" && action != "restart" {
		writeError(w, 404, "unknown action")
		return
	}
	data, code, err := s.request(s.helper, "POST", s.helperBase+"/v1/core/"+action, nil, nil)
	proxyResponse(w, data, code, err)
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Profiles()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"profiles": p})
}
func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := paramID(w, r)
	if !ok {
		return
	}
	p, err := s.store.Profile(id)
	if err != nil {
		writeError(w, 404, "profile not found")
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var p Profile
	if !decodeJSON(w, r, &p, 21<<20) {
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		writeError(w, 400, "name is required")
		return
	}
	if p.URL != "" && p.Content == "" {
		content, err := fetchSubscription(r.Context(), p.URL, s.cfg.AllowPrivateSubscriptionSources)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		p.Content = content
		p.Source = "remote"
	} else if p.Source == "" {
		p.Source = "local"
	}
	if err := validateYAML(p.Content); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	created, err := s.store.CreateProfile(p)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, created)
}
func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := paramID(w, r)
	if !ok {
		return
	}
	p, err := s.store.Profile(id)
	if err != nil {
		writeError(w, 404, "profile not found")
		return
	}
	var in Profile
	if !decodeJSON(w, r, &in, 21<<20) {
		return
	}
	p.Name = in.Name
	p.URL = in.URL
	p.Content = in.Content
	if err = validateYAML(p.Content); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.store.UpdateProfile(p); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := paramID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteProfile(id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
func (s *Server) refreshProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := paramID(w, r)
	if !ok {
		return
	}
	p, err := s.store.Profile(id)
	if err != nil || p.URL == "" {
		writeError(w, 400, "profile has no remote URL")
		return
	}
	p.Content, err = fetchSubscription(r.Context(), p.URL, s.cfg.AllowPrivateSubscriptionSources)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.store.UpdateProfile(p); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) activateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := paramID(w, r)
	if !ok {
		return
	}
	p, err := s.store.Profile(id)
	if err != nil {
		writeError(w, 404, "profile not found")
		return
	}
	data, code, err := s.request(s.helper, "POST", s.helperBase+"/v1/config/validate-activate", strings.NewReader(p.Content), map[string]string{"Content-Type": "application/yaml"})
	if err != nil || code >= 400 {
		proxyResponse(w, data, code, err)
		return
	}
	if err = s.store.ActivateProfile(id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"active": id})
}

func (s *Server) proxyMihomo(method, target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { s.forwardMihomo(w, r, method, target) }
}
func (s *Server) proxyHelper(method, target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := http.MaxBytesReader(w, r.Body, 21<<20)
		data, code, err := s.request(s.helper, method, s.helperBase+target, body, map[string]string{"Content-Type": r.Header.Get("Content-Type")})
		proxyResponse(w, data, code, err)
	}
}
func (s *Server) proxyDynamic(method, target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resolved := target
		for _, key := range []string{"name", "id"} {
			if v := chi.URLParam(r, key); v != "" {
				resolved = strings.ReplaceAll(resolved, "{"+key+"}", url.PathEscape(v))
			}
		}
		s.forwardMihomo(w, r, method, resolved)
	}
}
func (s *Server) forwardMihomo(w http.ResponseWriter, r *http.Request, method, target string) {
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	body := http.MaxBytesReader(w, r.Body, 2<<20)
	data, code, err := s.request(s.mihomo, method, s.mihomoBase+target, body, map[string]string{"Content-Type": r.Header.Get("Content-Type")})
	proxyResponse(w, data, code, err)
}

func (s *Server) websocketProxy(w http.ResponseWriter, r *http.Request) {
	topic := chi.URLParam(r, "topic")
	paths := map[string]string{"traffic": "/traffic", "memory": "/memory", "connections": "/connections", "logs": "/logs"}
	target, ok := paths[topic]
	if !ok {
		writeError(w, 404, "unknown stream")
		return
	}
	upstream, _, err := websocket.Dial(r.Context(), strings.Replace(s.mihomoBase, "http", "ws", 1)+target, &websocket.DialOptions{HTTPClient: s.mihomo})
	if err != nil {
		writeError(w, 502, "core stream unavailable")
		return
	}
	defer upstream.CloseNow()
	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{r.Host}})
	if err != nil {
		return
	}
	defer client.CloseNow()
	for {
		kind, data, err := upstream.Read(r.Context())
		if err != nil {
			return
		}
		if err = client.Write(r.Context(), kind, data); err != nil {
			return
		}
	}
}

func (s *Server) request(client *http.Client, method, target string, body io.Reader, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20))
	return data, resp.StatusCode, err
}

func socketHTTPClient(endpoint string, timeout time.Duration) (*http.Client, string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, "", err
	}
	transport := &http.Transport{Proxy: nil}
	base := endpoint
	if u.Scheme == "unix" {
		socketPath := u.Path
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}
		base = "http://unix"
	} else if u.Scheme == "tcp" {
		base = "http://" + u.Host
	}
	return &http.Client{Transport: transport, Timeout: timeout}, strings.TrimSuffix(base, "/"), nil
}

func spaHandler() http.Handler {
	sub, _ := fs.Sub(webassets.Dist, "dist")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "." {
			name = "index.html"
		}
		file, err := sub.Open(name)
		if err != nil {
			file, err = sub.Open("index.html")
		}
		if err != nil {
			http.Error(w, "UI unavailable", 500)
			return
		}
		defer file.Close()
		if ext := filepath.Ext(name); ext != "" {
			w.Header().Set("Content-Type", mime.TypeByExtension(ext))
		}
		_, _ = io.Copy(w, file)
	})
}

func ResetPassword(cfg Config) (string, error) {
	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		return "", err
	}
	defer store.Close()
	password, err := generatePassword()
	if err != nil {
		return "", err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return "", err
	}
	if err = store.ReplaceAdminPassword(hash); err != nil {
		return "", err
	}
	return password, writeBootstrapPassword(cfg.DataDir, password)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}
func proxyResponse(w http.ResponseWriter, data []byte, code int, err error) {
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if code == 0 {
		code = 502
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}
func paramID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return 0, false
	}
	return id, true
}
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func (s *Server) allowLogin(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	cut := time.Now().Add(-10 * time.Minute)
	a := s.loginAttempts[ip]
	n := a[:0]
	for _, t := range a {
		if t.After(cut) {
			n = append(n, t)
		}
	}
	s.loginAttempts[ip] = n
	return len(n) < 8
}
func (s *Server) recordLogin(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	s.loginAttempts[ip] = append(s.loginAttempts[ip], time.Now())
}
func init() { _ = sql.ErrNoRows; _ = fmt.Sprintf }
