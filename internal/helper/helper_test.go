package helper

import "testing"

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
