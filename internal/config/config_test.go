package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gateway.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const validConfig = `
server:
  addr: ":9999"
  read_timeout: 3s
upstreams:
  - name: sms
    target: "http://localhost:9001"
    timeout: 2s
routes:
  - path_prefix: /v1/sms
    upstream: sms
    methods: [GET]
plans:
  free: { rate: 5, burst: 10 }
clients:
  - { api_key: "k1", name: "acme", plan: free }
`

func TestLoad_Valid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Addr != ":9999" {
		t.Errorf("addr = %q, want :9999", cfg.Server.Addr)
	}
	if cfg.Server.ReadTimeout.Std() != 3*time.Second {
		t.Errorf("read_timeout = %v, want 3s", cfg.Server.ReadTimeout.Std())
	}
	if cfg.RateLimit.Backend != "memory" {
		t.Errorf("default backend = %q, want memory", cfg.RateLimit.Backend)
	}
}

func TestLoad_Rejects(t *testing.T) {
	cases := map[string]string{
		"unknown upstream": `
upstreams:
  - { name: sms, target: "http://localhost:9001" }
routes:
  - { path_prefix: /x, upstream: nope }
`,
		"unknown plan": `
upstreams:
  - { name: sms, target: "http://localhost:9001" }
routes:
  - { path_prefix: /x, upstream: sms }
plans:
  free: { rate: 5, burst: 10 }
clients:
  - { api_key: "k1", name: "acme", plan: gold }
`,
		"bad duration": `
upstreams:
  - { name: sms, target: "http://localhost:9001", timeout: "soon" }
routes:
  - { path_prefix: /x, upstream: sms }
`,
		"bad target": `
upstreams:
  - { name: sms, target: "not-a-url" }
routes:
  - { path_prefix: /x, upstream: sms }
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); err == nil {
				t.Fatalf("expected an error for %q, got nil", name)
			}
		})
	}
}
