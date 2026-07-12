package helper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maris/clash-web/internal/app"
	"gopkg.in/yaml.v3"
)

func TestSanitizeProviderPaths(t *testing.T) {
	valid := map[string]any{"provider": map[string]any{"path": "providers/demo.yaml"}}
	if err := sanitizeProviderPaths(valid); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", "/etc/passwd", `..\secret`} {
		bad := map[string]any{"provider": map[string]any{"path": path}}
		if err := sanitizeProviderPaths(bad); err == nil {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
}

func TestPrepareAppliesPersistentOverrides(t *testing.T) {
	runtimeDir := t.TempDir()
	overrides := []byte("allow-lan: true\nmixed-port: 7890\ndns:\n  enable: true\n  ipv6: true\n")
	if err := os.WriteFile(filepath.Join(runtimeDir, "overrides.yaml"), overrides, 0o640); err != nil {
		t.Fatal(err)
	}
	m := &manager{cfg: app.Config{RuntimeDir: runtimeDir, MihomoController: "unix:///tmp/mihomo.sock"}}
	prepared, err := m.prepare([]byte("mode: rule\ndns:\n  enable: false\n  listen: 0.0.0.0:1053\n"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = yaml.Unmarshal(prepared, &config); err != nil {
		t.Fatal(err)
	}
	if config["allow-lan"] != true || config["mixed-port"] != 7890 {
		t.Fatalf("top-level overrides not applied: %#v", config)
	}
	dns := config["dns"].(map[string]any)
	if dns["enable"] != true || dns["ipv6"] != true || dns["listen"] != "0.0.0.0:1053" {
		t.Fatalf("nested DNS overrides not merged: %#v", dns)
	}
}

func TestMaintenanceIdentifiersAndHosts(t *testing.T) {
	for _, id := range []string{"bundled", "v1.19.27", "v2.0.0-alpha"} {
		if !validCoreID(id) {
			t.Fatalf("valid core id rejected: %s", id)
		}
	}
	for _, id := range []string{"../mihomo", "/tmp/core", "latest", "v1/evil"} {
		if validCoreID(id) {
			t.Fatalf("unsafe core id accepted: %s", id)
		}
	}
	if !trustedGitHubHost("release-assets.githubusercontent.com") || trustedGitHubHost("github.com.example.org") {
		t.Fatal("maintenance host allowlist is incorrect")
	}
	if runtime.GOARCH == "amd64" && coreAssetName("v1.2.3") != "mihomo-linux-amd64-compatible-v1.2.3.gz" {
		t.Fatal("amd64 updater must use the baseline-compatible asset")
	}
}
