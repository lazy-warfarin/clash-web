package app

import (
	"net/netip"
	"testing"
)

func TestBlockedIP(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1"} {
		if !blockedIP(netip.MustParseAddr(raw)) {
			t.Fatalf("expected %s to be blocked", raw)
		}
	}
	if blockedIP(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public IP blocked")
	}
}

func TestValidateYAML(t *testing.T) {
	if err := validateYAML("proxies: []\nmode: rule\n"); err != nil {
		t.Fatal(err)
	}
	if err := validateYAML(": bad"); err == nil {
		t.Fatal("invalid YAML accepted")
	}
}
