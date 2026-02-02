package core

import (
	"fmt"
	"log"
	"network-router/pkg/utils"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func getResolvedIPsFilePath() string {
	return filepath.Join(os.TempDir(), ".resolved_ips.yaml")
}

// Router handles network routing operations
type Router struct {
	config       *Config
	wifiIface    *utils.InterfaceInfo
	phoneIface   *utils.InterfaceInfo
	resolvedIPs  []string
	wifiGateway  string
	phoneGateway string
}

// NewRouter creates a new Router instance
func NewRouter(config *Config) (*Router, error) {
	return &Router{
		config: config,
	}, nil
}

// DetectInterfaces detects WiFi and Phone interfaces
func (r *Router) DetectInterfaces() error {
	interfaces, err := utils.GetNetworkInterfaces()
	if err != nil {
		return fmt.Errorf("error getting interfaces: %w", err)
	}

	// Use defaults if not in config
	wifiKw := r.config.WifiInterfaceKeyword
	if wifiKw == "" {
		wifiKw = "Wi-Fi"
	}
	phoneKw := r.config.PhoneInterfaceKeyword
	if phoneKw == "" {
		phoneKw = "iPhone USB"
	}

	r.wifiIface = utils.FindInterfaceByName(interfaces, []string{wifiKw, "Wi-Fi"})
	r.phoneIface = utils.FindInterfaceByName(interfaces, []string{phoneKw, "iPhone USB", "iPad USB", "RNDIS"})

	if r.wifiIface == nil {
		return fmt.Errorf("could not find Wi-Fi interface (keyword: %s)", wifiKw)
	}

	if r.phoneIface == nil {
		return fmt.Errorf("could not find Phone Tethering interface (keyword: %s)", phoneKw)
	}

	return nil
}

// HasBothInterfaces checks if both WiFi and Phone interfaces are active
func (r *Router) HasBothInterfaces() bool {
	return r.wifiIface != nil && r.phoneIface != nil &&
		utils.IsInterfaceActive(r.wifiIface.DeviceName) &&
		utils.IsInterfaceActive(r.phoneIface.DeviceName)
}

// GetInterfaceStatus returns current interface status
func (r *Router) GetInterfaceStatus() (wifiActive, phoneActive bool) {
	if r.wifiIface != nil {
		wifiActive = utils.IsInterfaceActive(r.wifiIface.DeviceName)
	}
	if r.phoneIface != nil {
		phoneActive = utils.IsInterfaceActive(r.phoneIface.DeviceName)
	}
	return
}

// ResolveDomains resolves all configured domains to IPs
func (r *Router) ResolveDomains() error {
	log.Println("Resolving tethering domains...")
	r.resolvedIPs = []string{}

	// Check if both interfaces are active - potential for DNS conflicts
	wifiActive := r.wifiIface != nil && utils.IsInterfaceActive(r.wifiIface.DeviceName)
	phoneActive := r.phoneIface != nil && utils.IsInterfaceActive(r.phoneIface.DeviceName)

	if wifiActive && phoneActive {
		log.Println("⚠️  Warning: Both WiFi and Phone tethering are active.")
		log.Println("   DNS resolution may experience timeouts due to network conflicts.")
		log.Println("   This is normal - routing will continue with successfully resolved domains and CIDRs.")
	}

	// Deduplicate domains
	uniqueDomains := make(map[string]bool)
	for _, d := range r.config.TetherDomains {
		uniqueDomains[d] = true
	}

	totalDomains := len(uniqueDomains)
	successCount := 0
	failedCount := 0
	var failedDomains []string

	for domain := range uniqueDomains {
		targetDomain := domain
		if strings.HasPrefix(domain, "*.") {
			targetDomain = strings.TrimPrefix(domain, "*.")
			log.Printf("  Wildcard domain detected '%s', attempting to resolve base domain '%s'\n", domain, targetDomain)
		}

		var ips []string
		var err error

		// If both interfaces are active, add retry logic for DNS resolution
		if wifiActive && phoneActive {
			maxRetries := 3
			for attempt := 1; attempt <= maxRetries; attempt++ {
				ips, err = utils.ResolveDomainToIPs(targetDomain)
				if err == nil {
					break
				}
				if attempt < maxRetries {
					log.Printf("  ⏳ DNS resolution attempt %d/%d failed for %s, retrying in 2s...", attempt, maxRetries, targetDomain)
					time.Sleep(2 * time.Second)
				}
			}
		} else {
			ips, err = utils.ResolveDomainToIPs(targetDomain)
		}

		if err != nil {
			failedCount++
			failedDomains = append(failedDomains, targetDomain)
			log.Printf("  ✗ Failed to resolve %s after retries", targetDomain)
			if wifiActive && phoneActive {
				log.Printf("    (Network conflict - this is expected when both interfaces are active)")
				log.Printf("    Routing will continue with successfully resolved domains and CIDRs")
			}
			continue
		}
		successCount++
		log.Printf("  ✓ Resolved %s -> %v\n", targetDomain, ips)
		r.resolvedIPs = append(r.resolvedIPs, ips...)
	}

	// Summary
	log.Printf("\n📊 DNS Resolution Summary:")
	log.Printf("   Total domains: %d", totalDomains)
	log.Printf("   Successfully resolved: %d (%.1f%%)", successCount, float64(successCount)/float64(totalDomains)*100)
	log.Printf("   Failed to resolve: %d (%.1f%%)", failedCount, float64(failedCount)/float64(totalDomains)*100)

	if failedCount > 0 && len(failedDomains) <= 5 {
		log.Printf("   Failed domains: %v", failedDomains)
	}

	// Only return error if NO domains resolved successfully
	if successCount == 0 && totalDomains > 0 {
		return fmt.Errorf("failed to resolve any domains (%d total). Check your network connectivity", totalDomains)
	}

	if failedCount > 0 {
		log.Println("\n💡 Tip: Failed resolutions are common with dual interfaces.")
		log.Println("   Your configured CIDRs and successfully resolved domains will still be routed correctly.")
	}

	return nil
}

// ApplyRoutes applies routing rules
func (r *Router) ApplyRoutes() error {
	log.Println("Applying routing rules...")

	// Get gateways
	var err error
	r.wifiGateway, err = utils.GetInterfaceGateway(r.wifiIface.DeviceName)
	if err != nil {
		log.Printf("Warning: Could not get Wifi gateway IP: %v", err)
	}

	r.phoneGateway, err = utils.GetInterfaceGateway(r.phoneIface.DeviceName)
	if err != nil {
		log.Printf("Warning: Could not get Phone gateway IP: %v", err)
	}

	// 1. Resolve Domains BEFORE switching gateway (using current/WiFi DNS)
	// This prevents DNS resolution issues when Phone network DNS is not working
	log.Println("\nResolving domains (using current DNS)...")
	if err := r.ResolveDomains(); err != nil {
		log.Printf("\n❌ Critical: %v", err)
		log.Println("Cannot continue without any resolved domains or CIDRs.")
		return err
	}

	// Report on what will be routed
	log.Printf("\n📋 Routing Plan:")
	log.Printf("   CIDRs to route: %d", len(r.config.TetherCIDRs))
	log.Printf("   Resolved IPs to route: %d", len(r.resolvedIPs))
	totalRoutes := len(r.config.TetherCIDRs) + len(r.resolvedIPs)
	if totalRoutes == 0 {
		log.Println("\n⚠️  Warning: No routes to apply (no CIDRs and no resolved IPs)")
		log.Println("   Please check your configuration or network connectivity.")
		return fmt.Errorf("no routes to apply")
	}
	log.Printf("   Total routes to apply: %d\n", totalRoutes)

	// 2. Configure routes (Skipped switching default gateway as per request)
	// We will add specific routes via Phone interface/gateway instead.

	// Route Tether CIDRs to Phone
	for _, cidr := range r.config.TetherCIDRs {
		if err := r.addPhoneRoute(cidr); err != nil {
			log.Printf("Error adding route for %s: %v", cidr, err)
		} else {
			log.Printf("✓ Added route for %s via Phone\n", cidr)
		}
	}

	// Route Resolved Domain IPs to Phone
	for _, ip := range r.resolvedIPs {
		target := ip
		if !isCIDR(target) {
			target = ip + "/32"
		}

		if err := r.addPhoneRoute(target); err != nil {
			log.Printf("Error adding route for IP %s: %v", target, err)
		} else {
			log.Printf("✓ Added route for %s via Phone\n", target)
		}
	}

	// Save resolved IPs for later cleanup
	if err := r.saveResolvedIPs(); err != nil {
		log.Printf("Warning: Could not save resolved IPs: %v", err)
	}

	log.Println("Routing configuration completed successfully!")
	return nil
}

// ClearRoutes clears all routing rules
func (r *Router) ClearRoutes() error {
	log.Println("Cleaning up routing configuration...")

	// Reset Default Gateway to Wifi
	if r.wifiIface != nil {
		wifiGateway, err := utils.GetInterfaceGateway(r.wifiIface.DeviceName)
		if err == nil && wifiGateway != "" {
			log.Printf("Resetting default gateway to Wifi Gateway (%s)...\n", wifiGateway)
			if err := utils.ChangeDefaultGateway(wifiGateway); err != nil {
				log.Printf("Failed to reset default gateway: %v", err)
			} else {
				log.Println("✓ Default gateway reset to Wi-Fi.")
			}
		}
	}

	// Try to load saved IPs for more accurate cleanup
	saved, err := loadResolvedIPs()
	if err == nil {
		log.Println("Using saved IP list for cleanup...")
		for _, cidr := range saved.CIDRs {
			if err := utils.DeleteRoute(cidr); err == nil {
				log.Printf("✓ Deleted route for %s (saved)\n", cidr)
			}
		}
		for _, ip := range saved.IPs {
			target := ip
			if !isCIDR(target) {
				target = ip + "/32"
			}
			if err := utils.DeleteRoute(target); err == nil {
				log.Printf("✓ Deleted route for IP %s (saved)\n", target)
			}
		}
		os.Remove(getResolvedIPsFilePath())
		log.Println("Cleanup completed from saved file!")
		return nil
	}

	// Fallback to config
	log.Println("No saved IP list found. Falling back to current configuration...")

	for _, cidr := range r.config.TetherCIDRs {
		if err := utils.DeleteRoute(cidr); err == nil {
			log.Printf("✓ Deleted route for %s\n", cidr)
		}
	}

	// Resolve and delete domain routes
	if len(r.resolvedIPs) == 0 {
		r.ResolveDomains()
	}

	for _, ip := range r.resolvedIPs {
		target := ip
		if !isCIDR(target) {
			target = ip + "/32"
		}
		if err := utils.DeleteRoute(target); err == nil {
			log.Printf("✓ Deleted route for IP %s\n", target)
		}
	}

	log.Println("Cleanup completed!")
	return nil
}

// AddDynamicRoute adds a route for a single IP dynamically (used by DNS Proxy)
func (r *Router) AddDynamicRoute(ip string) error {
	target := ip
	if !isCIDR(target) {
		target = ip + "/32"
	}

	// Check if already in resolvedIPs to avoid duplicate routes
	for _, existingIP := range r.resolvedIPs {
		if existingIP == ip {
			return nil // Already routed
		}
	}

	log.Printf("🚀 Dynamic Routing: Adding route for %s via Phone\n", target)
	if err := r.addPhoneRoute(target); err != nil {
		return err
	}

	r.resolvedIPs = append(r.resolvedIPs, ip)
	
	// Incrementally update the saved file
	return r.saveResolvedIPs()
}

// Helper functions

func (r *Router) addPhoneRoute(target string) error {
	if r.phoneGateway != "" {
		return utils.AddRouteViaGateway(target, r.phoneGateway)
	}
	return utils.AddRoute(target, r.phoneIface.DeviceName)
}

func (r *Router) saveResolvedIPs() error {
	data := ResolvedIPs{
		IPs:   r.resolvedIPs,
		CIDRs: r.config.TetherCIDRs,
	}
	bytes, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	filePath := getResolvedIPsFilePath()
	log.Printf("Saving resolved IPs to: %s", filePath)
	return os.WriteFile(filePath, bytes, 0644)
}

func loadResolvedIPs() (*ResolvedIPs, error) {
	bytes, err := os.ReadFile(getResolvedIPsFilePath())
	if err != nil {
		return nil, err
	}
	var data ResolvedIPs
	err = yaml.Unmarshal(bytes, &data)
	return &data, err
}

func isCIDR(s string) bool {
	return strings.Contains(s, "/")
}
