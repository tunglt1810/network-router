package core

import (
	"os"

	"gopkg.in/yaml.v3"
)

// StatePath is where applied routes are recorded so they can be removed
// after a restart or a crash. The location must be stable across runs, which
// rules out os.TempDir(): TMPDIR differs between a root LaunchDaemon and an
// interactive sudo shell, so a record written by one would be invisible to the
// other.
const StatePath = "/usr/local/var/network-router/state.yaml"

// ProxyPort is the loopback port the DNS proxy serves and the resolver files name.
const ProxyPort = 5454

// DefaultPinnedUpstreams are public resolvers used when the tethered network
// does not advertise a working DNS server. Each one gets a host route over the
// tethered path so the query cannot leak back onto the bypassed network.
var DefaultPinnedUpstreams = []string{"1.1.1.1:53", "8.8.8.8:53"}

// Config represents the application configuration
type Config struct {
	TetherDomains         []string `yaml:"tether_domains"`
	TetherCIDRs           []string `yaml:"tether_cidrs"`
	PhoneInterfaceKeyword string   `yaml:"phone_interface_name"`

	// DNSUpstreamPinned replaces DefaultPinnedUpstreams when set.
	DNSUpstreamPinned []string `yaml:"dns_upstream_pinned"`
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

// PinnedUpstreams returns the fallback resolvers to reach over the tethered path.
func (c *Config) PinnedUpstreams() []string {
	if len(c.DNSUpstreamPinned) > 0 {
		return c.DNSUpstreamPinned
	}
	return DefaultPinnedUpstreams
}
