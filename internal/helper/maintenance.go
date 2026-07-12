package helper

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	mihomoLatestAPI  = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"
	geodataLatestAPI = "https://api.github.com/repos/MetaCubeX/meta-rules-dat/releases/latest"
)

type releaseAsset struct {
	Name               string `json:"name"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type coreInfo struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Selected bool   `json:"selected"`
	Bundled  bool   `json:"bundled"`
	Path     string `json:"-"`
}

func (m *manager) cores(w http.ResponseWriter, r *http.Request) {
	cores, selected, err := m.availableCores()
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"cores": cores, "selected": selected})
}

func (m *manager) selectCoreHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid core selection"})
		return
	}
	m.maintenanceMu.Lock()
	defer m.maintenanceMu.Unlock()
	if err := m.selectCore(input.ID); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *manager) updateCore(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Select bool `json:"select"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input)
	m.maintenanceMu.Lock()
	defer m.maintenanceMu.Unlock()

	var release githubRelease
	if err := m.fetchJSON(r.Context(), mihomoLatestAPI, &release); err != nil {
		write(w, http.StatusBadGateway, map[string]string{"error": "check official mihomo release: " + err.Error()})
		return
	}
	if !validCoreID(release.TagName) {
		write(w, http.StatusBadGateway, map[string]string{"error": "official release returned an invalid version"})
		return
	}
	wanted := coreAssetName(release.TagName)
	asset, ok := findAsset(release.Assets, wanted)
	if !ok {
		write(w, http.StatusBadGateway, map[string]string{"error": "official release has no compatible " + wanted + " asset"})
		return
	}
	compressed, err := m.download(r.Context(), asset, 80<<20)
	if err != nil {
		write(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		write(w, http.StatusBadGateway, map[string]string{"error": "invalid core archive: " + err.Error()})
		return
	}
	binary, err := io.ReadAll(io.LimitReader(reader, 120<<20))
	_ = reader.Close()
	if err != nil || len(binary) == 0 || len(binary) >= 120<<20 {
		write(w, http.StatusBadGateway, map[string]string{"error": "invalid or oversized core binary"})
		return
	}
	directory := filepath.Join(m.cfg.DataDir, "cores", release.TagName)
	if err = os.MkdirAll(directory, 0o750); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	corePath := filepath.Join(directory, "mihomo")
	temporary := corePath + ".tmp"
	if err = os.WriteFile(temporary, binary, 0o755); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if output, commandErr := exec.Command(temporary, "-v").CombinedOutput(); commandErr != nil {
		_ = os.Remove(temporary)
		write(w, http.StatusBadGateway, map[string]string{"error": "downloaded core failed to execute: " + strings.TrimSpace(string(output))})
		return
	}
	if _, statErr := os.Stat(filepath.Join(m.cfg.RuntimeDir, "active.yaml")); statErr == nil {
		if err = m.validateWith(temporary, filepath.Join(m.cfg.RuntimeDir, "active.yaml")); err != nil {
			_ = os.Remove(temporary)
			write(w, http.StatusBadGateway, map[string]string{"error": "downloaded core rejected the active configuration: " + err.Error()})
			return
		}
	}
	backup := corePath + ".previous"
	hadPrevious := false
	if _, statErr := os.Stat(corePath); statErr == nil {
		_ = os.Remove(backup)
		if err = os.Rename(corePath, backup); err != nil {
			_ = os.Remove(temporary)
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		hadPrevious = true
	}
	if err = os.Rename(temporary, corePath); err != nil {
		_ = os.Remove(temporary)
		if hadPrevious {
			_ = os.Rename(backup, corePath)
		}
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if input.Select {
		if err = m.selectCore(release.TagName); err != nil {
			m.stop()
			_ = os.Remove(corePath)
			if hadPrevious {
				_ = os.Rename(backup, corePath)
			}
			_ = m.start()
			write(w, http.StatusBadRequest, map[string]string{"error": "core installed but could not be selected: " + err.Error()})
			return
		}
	}
	_ = os.Remove(backup)
	cores, selected, _ := m.availableCores()
	write(w, http.StatusOK, map[string]any{"installed": release.TagName, "selected": selected, "cores": cores})
}

func (m *manager) updateGeoData(w http.ResponseWriter, r *http.Request) {
	m.maintenanceMu.Lock()
	defer m.maintenanceMu.Unlock()

	var release githubRelease
	if err := m.fetchJSON(r.Context(), geodataLatestAPI, &release); err != nil {
		write(w, http.StatusBadGateway, map[string]string{"error": "check official GeoData release: " + err.Error()})
		return
	}
	targets := []struct{ asset, file string }{
		{"geoip.metadb", "geoip.metadb"},
		{"geosite.dat", "GeoSite.dat"},
		{"country.mmdb", "Country.mmdb"},
	}
	type geoFile struct {
		name string
		data []byte
	}
	downloaded := make([]geoFile, 0, len(targets))
	updated := make([]string, 0, len(targets))
	for _, target := range targets {
		asset, ok := findAsset(release.Assets, target.asset)
		if !ok {
			if target.asset == "country.mmdb" {
				continue
			}
			write(w, http.StatusBadGateway, map[string]string{"error": "official GeoData release is missing " + target.asset})
			return
		}
		data, err := m.download(r.Context(), asset, 40<<20)
		if err != nil {
			write(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		downloaded = append(downloaded, geoFile{name: target.file, data: data})
	}
	for _, file := range downloaded {
		if err := atomicWrite(filepath.Join(m.cfg.RuntimeDir, file.name), file.data, 0o640); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		updated = append(updated, file.name)
	}
	m.stop()
	if err := m.start(); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "GeoData updated but core restart failed: " + err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"version": release.TagName, "updated": updated})
}

func (m *manager) availableCores() ([]coreInfo, string, error) {
	selected := m.selectedCoreID()
	result := []coreInfo{{ID: "bundled", Path: m.cfg.MihomoBinary, Bundled: true}}
	entries, err := os.ReadDir(filepath.Join(m.cfg.DataDir, "cores"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, selected, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validCoreID(entry.Name()) {
			continue
		}
		binary := filepath.Join(m.cfg.DataDir, "cores", entry.Name(), "mihomo")
		if info, statErr := os.Stat(binary); statErr == nil && info.Mode().IsRegular() {
			result = append(result, coreInfo{ID: entry.Name(), Path: binary})
		}
	}
	for index := range result {
		result[index].Selected = result[index].ID == selected
		result[index].Version = coreVersion(result[index].Path)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, selected, nil
}

func (m *manager) selectCore(id string) error {
	cores, previous, err := m.availableCores()
	if err != nil {
		return err
	}
	var chosen *coreInfo
	for index := range cores {
		if cores[index].ID == id {
			chosen = &cores[index]
			break
		}
	}
	if chosen == nil {
		return errors.New("unknown installed core")
	}
	active := filepath.Join(m.cfg.RuntimeDir, "active.yaml")
	if err = m.validateWith(chosen.Path, active); err != nil {
		return err
	}
	m.stop()
	if err = atomicWrite(filepath.Join(m.cfg.RuntimeDir, "selected-core"), []byte(id+"\n"), 0o640); err != nil {
		_ = m.start()
		return err
	}
	if err = m.start(); err != nil {
		_ = atomicWrite(filepath.Join(m.cfg.RuntimeDir, "selected-core"), []byte(previous+"\n"), 0o640)
		_ = m.start()
		return err
	}
	return nil
}

func (m *manager) selectedCoreID() string {
	data, err := os.ReadFile(filepath.Join(m.cfg.RuntimeDir, "selected-core"))
	if err != nil || !validCoreID(strings.TrimSpace(string(data))) {
		return "bundled"
	}
	return strings.TrimSpace(string(data))
}

func (m *manager) coreBinary() string {
	id := m.selectedCoreID()
	if id == "bundled" {
		return m.cfg.MihomoBinary
	}
	path := filepath.Join(m.cfg.DataDir, "cores", id, "mihomo")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path
	}
	return m.cfg.MihomoBinary
}

func (m *manager) fetchJSON(ctx context.Context, endpoint string, result any) error {
	data, err := m.fetchURL(ctx, endpoint, 4<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}

func (m *manager) download(ctx context.Context, asset releaseAsset, limit int64) ([]byte, error) {
	if asset.Size <= 0 || asset.Size > limit {
		return nil, fmt.Errorf("asset %s has an invalid size", asset.Name)
	}
	if !strings.HasPrefix(strings.ToLower(asset.Digest), "sha256:") {
		return nil, fmt.Errorf("asset %s has no SHA-256 digest", asset.Name)
	}
	data, err := m.fetchURL(ctx, asset.BrowserDownloadURL, limit)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset.Name, err)
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimPrefix(strings.ToLower(asset.Digest), "sha256:")) {
		return nil, fmt.Errorf("SHA-256 mismatch for %s", asset.Name)
	}
	return data, nil
}

func (m *manager) fetchURL(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || !trustedGitHubHost(u.Hostname()) {
		return nil, errors.New("untrusted maintenance URL")
	}
	clients := make([]*http.Client, 0, 2)
	if proxyURL := m.localProxyURL(); proxyURL != nil {
		clients = append(clients, maintenanceClient(proxyURL))
	}
	clients = append(clients, maintenanceClient(nil))
	var lastErr error
	for _, client := range clients {
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if requestErr != nil {
			return nil, requestErr
		}
		req.Header.Set("User-Agent", "clash-web-maintenance/1")
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || int64(len(data)) > limit {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

func (m *manager) localProxyURL() *url.URL {
	config, err := readYAMLMap(filepath.Join(m.cfg.RuntimeDir, "active.yaml"))
	if err != nil {
		return nil
	}
	port := number(config["mixed-port"])
	if port == 0 {
		port = number(config["port"])
	}
	if port < 1 || port > 65535 {
		return nil
	}
	u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	return u
}

func maintenanceClient(proxy *url.URL) *http.Client {
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	timeout := 45 * time.Second
	if proxy == nil {
		transport.Proxy = http.ProxyFromEnvironment
	} else {
		timeout = 2 * time.Minute
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" || !trustedGitHubHost(req.URL.Hostname()) {
			return errors.New("untrusted maintenance redirect")
		}
		return nil
	}}
}

func trustedGitHubHost(host string) bool {
	host = strings.ToLower(host)
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, asset := range assets {
		if strings.EqualFold(asset.Name, name) {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func validCoreID(id string) bool {
	if id == "bundled" {
		return true
	}
	if len(id) < 2 || len(id) > 64 || id[0] != 'v' {
		return false
	}
	for _, char := range id[1:] {
		if (char < '0' || char > '9') && char != '.' && char != '-' && (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			return false
		}
	}
	return true
}

func coreAssetName(version string) string {
	if runtime.GOARCH == "amd64" {
		return fmt.Sprintf("mihomo-linux-amd64-compatible-%s.gz", version)
	}
	return fmt.Sprintf("mihomo-linux-%s-%s.gz", runtime.GOARCH, version)
}

func coreVersion(binary string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "-v").CombinedOutput()
	if err != nil {
		return "unavailable"
	}
	line := strings.TrimSpace(string(output))
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	return line
}

func number(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
