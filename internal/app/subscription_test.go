package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"
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

	if _, err := fetchSubscription(context.Background(), server.URL, true, fetchOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestFetchSubscriptionCustomUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "MyCustom/1.0" {
			t.Errorf("unexpected User-Agent %q", got)
		}
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer server.Close()

	opt := fetchOptions{UserAgent: "MyCustom/1.0"}
	if _, err := fetchSubscription(context.Background(), server.URL, true, opt); err != nil {
		t.Fatal(err)
	}
}

func TestFetchSubscriptionTimeout(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer server.Close()
	defer close(block)

	opt := fetchOptions{Timeout: 300 * time.Millisecond}
	start := time.Now()
	if _, err := fetchSubscription(context.Background(), server.URL, true, opt); err == nil {
		t.Fatal("slow upstream should time out")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout not applied, took %s", elapsed)
	}
}

// TestFetchSubscriptionThroughProxy verifies the with_proxy path: the request
// goes through the configured local proxy and the SSRF guard is bypassed for
// the proxied destination.
func TestFetchSubscriptionThroughProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer target.Close()

	requests := make(chan *http.Request, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	opt := fetchOptions{ProxyURL: proxyURL}
	// allowPrivate=false would normally block the httptest loopback address;
	// with a proxy configured the fetch must succeed anyway.
	if _, err = fetchSubscription(context.Background(), target.URL, false, opt); err != nil {
		t.Fatalf("proxied fetch failed: %v", err)
	}
	select {
	case req := <-requests:
		if req.URL.Host == "" {
			t.Fatal("proxy received non-absolute request URI")
		}
	default:
		t.Fatal("request did not go through the proxy")
	}
}

func TestFetchSubscriptionProxyUnreachable(t *testing.T) {
	// Port 1 on loopback: connection refused.
	opt := fetchOptions{ProxyURL: &url.URL{Scheme: "http", Host: "127.0.0.1:1"}}
	if _, err := fetchSubscription(context.Background(), "http://example.com/sub", false, opt); err == nil {
		t.Fatal("fetch through dead proxy should fail, not fall back to direct")
	}
}
