package core

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	TetherDomains         []string `yaml:"tether_domains"`
	TetherCIDRs           []string `yaml:"tether_cidrs"`
	WifiInterfaceKeyword  string   `yaml:"wifi_interface_name"`
	PhoneInterfaceKeyword string   `yaml:"phone_interface_name"`
	RouteRefreshCron      string   `yaml:"route_refresh_cron"` // Cron expression for scheduled refresh
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
	err = decoder.Decode(&cfg)
	return &cfg, err
}

// ResolvedIPs stores resolved IPs and CIDRs for cleanup
type ResolvedIPs struct {
	IPs   []string `yaml:"ips"`
	CIDRs []string `yaml:"cidrs"`
}