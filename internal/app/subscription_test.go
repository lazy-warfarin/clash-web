package app

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestFetchSubscriptionRequestsMihomoFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "Clash.Meta" {
			t.Errorf("unexpected User-Agent %q", got)
		}
		_, _ = w.Write([]byte("proxies: []\nmode: rule\n"))
	}))
	defer server.Close()

	if _, err := fetchSubscription(context.Background(), server.URL, true); err != nil {
		t.Fatal(err)
	}
}
