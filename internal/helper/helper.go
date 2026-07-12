package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maris/clash-web/internal/app"
	"gopkg.in/yaml.v3"
)

type manager struct {
	cfg           app.Config
	ctx           context.Context
	mu            sync.Mutex
	maintenanceMu sync.Mutex
	cmd           *exec.Cmd
	done          chan struct{}
	started       time.Time
	lastExit      string
}

func Run(ctx context.Context, cfg app.Config) error {
	if err := os.MkdirAll(cfg.RuntimeDir, 0o770); err != nil {
		return err
	}
	m := &manager{cfg: cfg, ctx: ctx}
	r := chi.NewRouter()
	r.Get("/v1/status", m.status)
	r.Post("/v1/core/{action}", m.action)
	r.Post("/v1/config/validate-activate", m.activate)
	r.Get("/v1/config/current", m.currentConfig)
	r.Get("/v1/config/settings", m.currentSettings)
	r.Put("/v1/config/current", m.updateCurrentConfig)
	r.Patch("/v1/config/overrides", m.patchConfig)
	r.Get("/v1/cores", m.cores)
	r.Post("/v1/cores/update", m.updateCore)
	r.Post("/v1/cores/select", m.selectCoreHandler)
	r.Post("/v1/geodata/update", m.updateGeoData)
	listener, err := listen(cfg.HelperSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = server.Shutdown(context.Background()); m.stop() }()
	log.Printf("privileged helper listening on %s", cfg.HelperSocket)
	if _, statErr := os.Stat(filepath.Join(cfg.RuntimeDir, "active.yaml")); statErr == nil {
		if startErr := m.start(); startErr != nil {
			log.Printf("unable to restore active mihomo configuration: %v", startErr)
		}
	}
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func listen(endpoint string) (net.Listener, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "unix" {
		_ = os.Remove(u.Path)
		l, err := net.Listen("unix", u.Path)
		if err == nil {
			_ = os.Chmod(u.Path, 0o660)
		}
		return l, err
	}
	if u.Scheme == "tcp" {
		return net.Listen("tcp", u.Host)
	}
	return nil, fmt.Errorf("unsupported helper socket %q", endpoint)
}

func (m *manager) status(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	running := m.cmd != nil && m.cmd.Process != nil
	write(w, 200, map[string]any{"running": running, "pid": pid(m.cmd), "startedAt": m.started, "lastExit": m.lastExit, "selectedCore": m.selectedCoreID()})
}
func (m *manager) action(w http.ResponseWriter, r *http.Request) {
	switch chi.URLParam(r, "action") {
	case "start":
		if err := m.start(); err != nil {
			write(w, 500, map[string]string{"error": err.Error()})
			return
		}
	case "stop":
		m.stop()
	case "restart":
		m.stop()
		if err := m.start(); err != nil {
			write(w, 500, map[string]string{"error": err.Error()})
			return
		}
	default:
		write(w, 404, map[string]string{"error": "unknown action"})
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}

func (m *manager) activate(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, (20<<20)+1))
	if err != nil || len(data) > 20<<20 {
		write(w, 400, map[string]string{"error": "configuration is too large"})
		return
	}
	prepared, err := m.prepare(data)
	if err != nil {
		write(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err = m.activatePrepared(prepared); err != nil {
		write(w, 500, map[string]string{"error": err.Error()})
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}

func (m *manager) activatePrepared(prepared []byte) error {
	var err error
	candidate := filepath.Join(m.cfg.RuntimeDir, "candidate.yaml")
	active := filepath.Join(m.cfg.RuntimeDir, "active.yaml")
	backup := filepath.Join(m.cfg.RuntimeDir, "active.previous.yaml")
	if err = os.WriteFile(candidate, prepared, 0o640); err != nil {
		return err
	}
	if err = m.validate(candidate); err != nil {
		_ = os.Remove(candidate)
		return err
	}
	if _, err = os.Stat(active); err == nil {
		_ = copyFile(active, backup)
	}
	if err = os.Rename(candidate, active); err != nil {
		return err
	}
	m.stop()
	if err = m.start(); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = copyFile(backup, active)
			_ = m.start()
		}
		return errors.New("new configuration failed; previous version restored: " + err.Error())
	}
	return nil
}

func (m *manager) currentConfig(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(m.cfg.RuntimeDir, "active.yaml"))
	if err != nil {
		write(w, http.StatusNotFound, map[string]string{"error": "no active configuration"})
		return
	}
	write(w, http.StatusOK, map[string]string{"content": string(data)})
}

func (m *manager) currentSettings(w http.ResponseWriter, r *http.Request) {
	config, err := readYAMLMap(filepath.Join(m.cfg.RuntimeDir, "active.yaml"))
	if err != nil {
		write(w, http.StatusNotFound, map[string]string{"error": "no active configuration"})
		return
	}
	result := make(map[string]any)
	for _, key := range []string{"mode", "allow-lan", "bind-address", "ipv6", "mixed-port", "port", "socks-port", "redir-port", "tproxy-port", "tun", "dns"} {
		if value, ok := config[key]; ok {
			result[key] = value
		}
	}
	write(w, http.StatusOK, result)
}

func (m *manager) updateCurrentConfig(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, (20<<20)+1))
	if err != nil || len(data) > 20<<20 {
		write(w, http.StatusBadRequest, map[string]string{"error": "configuration is too large"})
		return
	}
	prepared, err := m.prepare(data)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err = m.activatePrepared(prepared); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *manager) patchConfig(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := decoder.Decode(&patch); err != nil || patch == nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid configuration patch"})
		return
	}
	overridesPath := filepath.Join(m.cfg.RuntimeDir, "overrides.yaml")
	overrides, err := readYAMLMap(overridesPath)
	if errors.Is(err, os.ErrNotExist) {
		overrides = map[string]any{}
	} else if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	mergeMaps(overrides, patch)
	active, err := readYAMLMap(filepath.Join(m.cfg.RuntimeDir, "active.yaml"))
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "no active configuration"})
		return
	}
	mergeMaps(active, overrides)
	prepared, err := m.prepareDocument(active)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err = m.activatePrepared(prepared); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	encoded, err := yaml.Marshal(overrides)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err = atomicWrite(overridesPath, encoded, 0o640); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *manager) prepare(data []byte) ([]byte, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	if overrides, err := readYAMLMap(filepath.Join(m.cfg.RuntimeDir, "overrides.yaml")); err == nil {
		mergeMaps(doc, overrides)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read configuration overrides: %w", err)
	}
	return m.prepareDocument(doc)
}

func (m *manager) prepareDocument(doc map[string]any) ([]byte, error) {
	if err := sanitizeProviderPaths(doc["proxy-providers"]); err != nil {
		return nil, err
	}
	if err := sanitizeProviderPaths(doc["rule-providers"]); err != nil {
		return nil, err
	}
	delete(doc, "external-ui")
	delete(doc, "external-ui-url")
	delete(doc, "external-controller-tls")
	controller := strings.TrimPrefix(m.cfg.MihomoController, "unix://")
	if strings.HasPrefix(m.cfg.MihomoController, "unix://") {
		doc["external-controller"] = ""
		doc["external-controller-unix"] = controller
	} else {
		u, _ := url.Parse(m.cfg.MihomoController)
		doc["external-controller"] = u.Host
	}
	doc["secret"] = ""
	return yaml.Marshal(doc)
}
func (m *manager) validate(file string) error {
	return m.validateWith(m.coreBinary(), file)
}
func (m *manager) validateWith(binary, file string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-t", "-d", m.cfg.RuntimeDir, "-f", file)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mihomo validation failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
func (m *manager) start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		return nil
	}
	active := filepath.Join(m.cfg.RuntimeDir, "active.yaml")
	if _, err := os.Stat(active); err != nil {
		return fmt.Errorf("no active configuration")
	}
	cmd := exec.Command(m.coreBinary(), "-d", m.cfg.RuntimeDir, "-f", active)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	m.cmd = cmd
	m.done = make(chan struct{})
	m.started = time.Now().UTC()
	go m.wait(cmd, m.done)
	return nil
}
func (m *manager) wait(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	m.mu.Lock()
	close(done)
	restart := false
	if m.cmd == cmd {
		m.cmd = nil
		m.done = nil
		if err != nil {
			m.lastExit = err.Error()
		} else {
			m.lastExit = "clean exit"
		}
		restart = m.ctx != nil && m.ctx.Err() == nil
	}
	m.mu.Unlock()
	if restart {
		go func() {
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(time.Second):
			}
			if err := m.start(); err != nil {
				log.Printf("unable to restart mihomo after exit: %v", err)
			}
		}()
	}
}
func (m *manager) stop() {
	m.mu.Lock()
	cmd := m.cmd
	done := m.done
	m.cmd = nil
	m.done = nil
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	signalProcessGroup(cmd)
	select {
	case <-done:
		return
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
	}
}
func pid(cmd *exec.Cmd) int {
	if cmd == nil || cmd.Process == nil {
		return 0
	}
	return cmd.Process.Pid
}
func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o640)
}

func readYAMLMap(file string) (map[string]any, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err = yaml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

func mergeMaps(target, patch map[string]any) {
	for key, value := range patch {
		if nestedPatch, ok := value.(map[string]any); ok {
			if nestedTarget, ok := target[key].(map[string]any); ok {
				mergeMaps(nestedTarget, nestedPatch)
				continue
			}
		}
		target[key] = value
	}
}

func atomicWrite(file string, data []byte, mode os.FileMode) error {
	temporary := file + ".tmp"
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, file)
}

func sanitizeProviderPaths(value any) error {
	providers, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for name, raw := range providers {
		provider, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		pathValue, ok := provider["path"].(string)
		if !ok || pathValue == "" {
			continue
		}
		portable := strings.ReplaceAll(pathValue, "\\", "/")
		clean := pathpkg.Clean(portable)
		if pathpkg.IsAbs(clean) || filepath.IsAbs(pathValue) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("provider %q path must stay inside the runtime directory", name)
		}
	}
	return nil
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
