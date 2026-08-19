package core

import (
	"os"

	"gopkg.in/yaml.v3"
)

// DefaultStatePath is where applied routes are recorded so they can be removed
// after a restart or a crash. The location must be stable across runs, which
// rules out os.TempDir(): TMPDIR differs between a root LaunchDaemon and an
// interactive sudo shell, so a record written by one would be invisible to the
// other.
const DefaultStatePath = "/usr/local/var/network-router/state.yaml"

// DefaultPinnedUpstreams are public resolvers used when the tethered network
// does not advertise a working DNS server. Each one gets a host route over the
// tethered path so the query cannot leak back onto the bypassed network.
var DefaultPinnedUpstreams = []string{"1.1.1.1:53", "8.8.8.8:53"}

// Config represents the application configuration
type Config struct {
	TetherDomains         []string `yaml:"tether_domains"`
	TetherCIDRs           []string `yaml:"tether_cidrs"`
	WifiInterfaceKeyword  string   `yaml:"wifi_interface_name"`
	PhoneInterfaceKeyword string   `yaml:"phone_interface_name"`
	RouteRefreshCron      string   `yaml:"route_refresh_cron"`
	AutoRefreshRoute      bool     `yaml:"auto_refresh_route"`
	DNSProxyEnabled       bool     `yaml:"dns_proxy_enabled"`
	DNSProxyPort          int      `yaml:"dns_proxy_port"`

	// DNSUpstream overrides upstream selection entirely when set.
	DNSUpstream string `yaml:"dns_upstream"`
	// DNSUpstreamPinned replaces DefaultPinnedUpstreams when set.
	DNSUpstreamPinned []string `yaml:"dns_upstream_pinned"`
	// ForceIPv4 makes the proxy answer AAAA, HTTPS and SVCB queries with an
	// empty NOERROR so clients use the A records that actually get routed.
	// Defaults to true when omitted.
	ForceIPv4 *bool `yaml:"force_ipv4"`
	// StatePath overrides DefaultStatePath.
	StatePath string `yaml:"state_path"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ForceIPv4Enabled reports whether IPv6-preferring query types should be
// answered empty. Absent configuration means enabled.
func (c *Config) ForceIPv4Enabled() bool {
	return c.ForceIPv4 == nil || *c.ForceIPv4
}

// PinnedUpstreams returns the fallback resolvers to reach over the tethered path.
func (c *Config) PinnedUpstreams() []string {
	if len(c.DNSUpstreamPinned) > 0 {
		return c.DNSUpstreamPinned
	}
	return DefaultPinnedUpstreams
}

// ProxyPort returns the configured DNS proxy port with its default applied.
func (c *Config) ProxyPort() int {
	if c.DNSProxyPort == 0 {
		return 5454
	}
	return c.DNSProxyPort
}

// ResolvedStatePath returns the path used to record applied routes.
func (c *Config) ResolvedStatePath() string {
	if c.StatePath != "" {
		return c.StatePath
	}
	return DefaultStatePath
}
