// Package config loads and validates the gateway configuration from YAML.
package config

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root of the gateway configuration.
type Config struct {
	Server    Server          `yaml:"server"`
	Upstreams []Upstream      `yaml:"upstreams"`
	Routes    []Route         `yaml:"routes"`
	RateLimit RateLimit       `yaml:"ratelimit"`
	Plans     map[string]Plan `yaml:"plans"`
	Clients   []Client        `yaml:"clients"`
}

// RateLimit configures the rate-limiting backend. Backend is "memory"
// (default, single instance) or "redis" (distributed).
type RateLimit struct {
	Backend   string `yaml:"backend"`
	RedisAddr string `yaml:"redis_addr"`
}

// Plan defines a rate-limit tier: rate tokens are added per second up to a
// bucket capacity of burst.
type Plan struct {
	Rate  float64 `yaml:"rate"`
	Burst int     `yaml:"burst"`
}

// Client is an API consumer identified by an API key and assigned a plan.
type Client struct {
	APIKey string `yaml:"api_key"`
	Name   string `yaml:"name"`
	Plan   string `yaml:"plan"`
}

// Server holds the HTTP server settings for the gateway itself.
type Server struct {
	Addr         string   `yaml:"addr"`
	ReadTimeout  Duration `yaml:"read_timeout"`
	WriteTimeout Duration `yaml:"write_timeout"`
}

// Upstream is a backend service the gateway can forward requests to.
type Upstream struct {
	Name           string         `yaml:"name"`
	Target         string         `yaml:"target"`
	Timeout        Duration       `yaml:"timeout"`
	Retry          Retry          `yaml:"retry"`
	CircuitBreaker CircuitBreaker `yaml:"circuit_breaker"`
}

// Retry controls automatic retries of failed upstream calls. Only safe
// (bodyless) requests are retried, to avoid replaying a request body.
type Retry struct {
	MaxAttempts int      `yaml:"max_attempts"` // extra attempts after the first; 0 disables
	Backoff     Duration `yaml:"backoff"`      // base delay, doubled each attempt
}

// CircuitBreaker trips an upstream open after FailureThreshold consecutive
// failures and rejects requests for Cooldown. A zero threshold disables it.
type CircuitBreaker struct {
	FailureThreshold int      `yaml:"failure_threshold"`
	Cooldown         Duration `yaml:"cooldown"`
}

// Route maps an incoming path prefix to an upstream.
type Route struct {
	PathPrefix string   `yaml:"path_prefix"`
	Upstream   string   `yaml:"upstream"`
	Methods    []string `yaml:"methods"`
}

// Load reads, parses, validates and defaults the config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.RateLimit.Backend == "" {
		c.RateLimit.Backend = "memory"
	}
	if c.RateLimit.RedisAddr == "" {
		c.RateLimit.RedisAddr = "localhost:6379"
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = Duration(5 * time.Second)
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = Duration(10 * time.Second)
	}
	for i := range c.Upstreams {
		u := &c.Upstreams[i]
		if u.Timeout == 0 {
			u.Timeout = Duration(5 * time.Second)
		}
		if u.Retry.Backoff == 0 {
			u.Retry.Backoff = Duration(50 * time.Millisecond)
		}
		if u.CircuitBreaker.FailureThreshold > 0 && u.CircuitBreaker.Cooldown == 0 {
			u.CircuitBreaker.Cooldown = Duration(5 * time.Second)
		}
	}
}

func (c *Config) validate() error {
	if len(c.Upstreams) == 0 {
		return fmt.Errorf("no upstreams defined")
	}
	known := make(map[string]bool, len(c.Upstreams))
	for _, u := range c.Upstreams {
		if u.Name == "" {
			return fmt.Errorf("upstream with empty name")
		}
		if known[u.Name] {
			return fmt.Errorf("duplicate upstream name %q", u.Name)
		}
		known[u.Name] = true
		parsed, err := url.Parse(u.Target)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("upstream %q has invalid target %q", u.Name, u.Target)
		}
		if u.Retry.MaxAttempts < 0 {
			return fmt.Errorf("upstream %q has negative retry.max_attempts", u.Name)
		}
		if u.CircuitBreaker.FailureThreshold < 0 {
			return fmt.Errorf("upstream %q has negative circuit_breaker.failure_threshold", u.Name)
		}
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("no routes defined")
	}
	for _, r := range c.Routes {
		if r.PathPrefix == "" {
			return fmt.Errorf("route with empty path_prefix")
		}
		if !known[r.Upstream] {
			return fmt.Errorf("route %q references unknown upstream %q", r.PathPrefix, r.Upstream)
		}
	}
	switch c.RateLimit.Backend {
	case "memory", "redis":
	default:
		return fmt.Errorf("ratelimit.backend must be \"memory\" or \"redis\", got %q", c.RateLimit.Backend)
	}
	for name, p := range c.Plans {
		if p.Rate <= 0 || p.Burst <= 0 {
			return fmt.Errorf("plan %q must have positive rate and burst", name)
		}
	}
	seenKeys := make(map[string]bool, len(c.Clients))
	for _, cl := range c.Clients {
		if cl.APIKey == "" {
			return fmt.Errorf("client %q has empty api_key", cl.Name)
		}
		if seenKeys[cl.APIKey] {
			return fmt.Errorf("duplicate api_key for client %q", cl.Name)
		}
		seenKeys[cl.APIKey] = true
		if _, ok := c.Plans[cl.Plan]; !ok {
			return fmt.Errorf("client %q references unknown plan %q", cl.Name, cl.Plan)
		}
	}
	return nil
}
