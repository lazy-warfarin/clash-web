package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen                          string `yaml:"listen"`
	DataDir                         string `yaml:"data_dir"`
	RuntimeDir                      string `yaml:"runtime_dir"`
	MihomoBinary                    string `yaml:"mihomo_binary"`
	MihomoController                string `yaml:"mihomo_controller"`
	HelperSocket                    string `yaml:"helper_socket"`
	AllowPrivateSubscriptionSources bool   `yaml:"allow_private_subscription_sources"`
	TrustedProxy                    bool   `yaml:"trusted_proxy"`
}

func DefaultConfig() Config {
	if runtime.GOOS == "windows" {
		return Config{Listen: "127.0.0.1:8080", DataDir: "data", RuntimeDir: "data/run", MihomoBinary: "mihomo.exe", MihomoController: "http://127.0.0.1:9090", HelperSocket: "tcp://127.0.0.1:9088"}
	}
	return Config{Listen: "0.0.0.0:8080", DataDir: "/var/lib/clash-web", RuntimeDir: "/var/lib/clash-web/runtime", MihomoBinary: "/usr/lib/clash-web/mihomo", MihomoController: "unix:///run/clash-web/mihomo.sock", HelperSocket: "unix:///run/clash-web/helper.sock"}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		path = os.Getenv("CLASH_WEB_CONFIG")
	}
	if path == "" && runtime.GOOS != "windows" {
		path = "/etc/clash-web/config.yaml"
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return cfg, err
		}
		if err == nil && len(data) > 0 {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return cfg, err
			}
		}
	}
	override(&cfg.Listen, "CLASH_WEB_LISTEN")
	override(&cfg.DataDir, "CLASH_WEB_DATA_DIR")
	override(&cfg.RuntimeDir, "CLASH_WEB_RUNTIME_DIR")
	override(&cfg.MihomoBinary, "CLASH_WEB_MIHOMO_BINARY")
	override(&cfg.MihomoController, "CLASH_WEB_MIHOMO_CONTROLLER")
	override(&cfg.HelperSocket, "CLASH_WEB_HELPER_SOCKET")
	if cfg.Listen == "" || cfg.DataDir == "" || cfg.RuntimeDir == "" {
		return cfg, errors.New("listen, data_dir and runtime_dir are required")
	}
	for _, dir := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "profiles"), filepath.Join(cfg.DataDir, "versions")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func override(target *string, key string) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		*target = value
	}
}
