package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// fetchOptions carries per-profile subscription download settings. The zero
// value matches the historical behaviour: default User-Agent, 25s timeout,
// direct connection with SSRF guards.
type fetchOptions struct {
	UserAgent string        // empty = "Clash.Meta"
	Timeout   time.Duration // zero = 25s
	ProxyURL  *url.URL      // nil = direct connection
}

func validateYAML(content string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("profile content is empty")
	}
	if len(content) > 20<<20 {
		return errors.New("profile exceeds 20 MiB")
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if node.Kind == 0 {
		return errors.New("profile is empty")
	}
	return nil
}

func fetchSubscription(ctx context.Context, rawURL string, allowPrivate bool, opt fetchOptions) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("subscription URL must use http or https")
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 25 * time.Second
	}
	if opt.UserAgent == "" {
		opt.UserAgent = "Clash.Meta"
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		// When fetching through the local mihomo port the actual outbound
		// connection is made by mihomo, so the destination resolved here is
		// only the proxy's loopback address; the private-range guard must not
		// apply to it.
		if opt.ProxyURL != nil {
			return dialer.DialContext(ctx, network, address)
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if !allowPrivate {
			for _, ip := range ips {
				if blockedIP(ip) {
					return nil, fmt.Errorf("subscription destination %s is private or reserved", ip)
				}
			}
		}
		return dialer.DialContext(ctx, network, address)
	}}
	if opt.ProxyURL != nil {
		transport.Proxy = http.ProxyURL(opt.ProxyURL)
	}
	client := &http.Client{Transport: transport, Timeout: opt.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	// Subscription providers commonly use the User-Agent to choose the output
	// format. Identify the compatible core so providers return a mihomo/Clash
	// configuration instead of a generic or legacy client payload.
	req.Header.Set("User-Agent", opt.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("subscription returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (20<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 20<<20 {
		return "", errors.New("subscription exceeds 20 MiB")
	}
	content := string(data)
	return content, validateYAML(content)
}

func blockedIP(ip netip.Addr) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}
