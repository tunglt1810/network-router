// Copyright 2026 bez
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"

	"network-router/client"
	"network-router/daemon"
	"network-router/tray"
)

func main() {
	// Define subcommands
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "daemon":
		runDaemon()
	case "tray":
		runTray()
	case "status":
		runClientCommand("status")
	case "enable":
		runClientCommand("enable")
	case "disable":
		runClientCommand("disable")
	case "apply":
		runClientCommand("apply")
	case "clear":
		runClientCommand("clear")
	case "restart":
		runClientCommand("restart")
	case "enable-dns":
		runClientCommand("enable-dns")
	case "disable-dns":
		runClientCommand("disable-dns")
	case "tray-enable":
		runTrayEnable()
	case "tray-disable":
		runTrayDisable()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func runDaemon() {
	daemonCmd := flag.NewFlagSet("daemon", flag.ExitOnError)
	configPath := daemonCmd.String("config", "config.yaml", "Path to configuration file")

	daemonCmd.Parse(os.Args[2:])

	d, err := daemon.NewDaemon(*configPath)
	if err != nil {
		log.Fatalf("Failed to create daemon: %v", err)
	}

	if err := d.Run(); err != nil {
		log.Fatalf("Daemon error: %v", err)
	}
}

func runTray() {
	app := tray.NewTrayApp()
	app.Run()
}

func runClientCommand(command string) {
	c := client.NewClient()

	var err error
	switch command {
	case "status":
		err = c.Status()
	case "enable":
		err = c.Enable()
	case "disable":
		err = c.Disable()
	case "apply":
		err = c.Apply()
	case "clear":
		err = c.Clear()
	case "restart":
		err = c.Restart()
	case "enable-dns":
		err = c.EnableDNSProxy()
	case "disable-dns":
		err = c.DisableDNSProxy()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runTrayEnable() {
	userHome, _ := os.UserHomeDir()
	uid := os.Getuid()
	plistDest := fmt.Sprintf("%s/Library/LaunchAgents/com.bez.network-router.tray.plist", userHome)
	plistTemplate := "/usr/local/etc/network-router/tray-agent.plist"
	label := "gui/%d/com.bez.network-router.tray"
	fullLabel := fmt.Sprintf(label, uid)

	fmt.Printf("Enabling tray icon for user %d...\n", uid)

	// 1. Ensure LaunchAgents directory exists
	_ = os.MkdirAll(fmt.Sprintf("%s/Library/LaunchAgents", userHome), 0755)

	// 2. Install plist from template if not already there
	if _, err := os.Stat(plistTemplate); err == nil {
		// Use cp command to maintain file permissions or just write it
		cmdCopy := exec.Command("cp", plistTemplate, plistDest)
		if err := cmdCopy.Run(); err != nil {
			fmt.Printf("Error: Could not install tray configuration: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("Error: Tray service template not found. Please run sudo ./install_service.sh first.\n")
		os.Exit(1)
	}

	// 3. Bootstrap (register)
	cmdRegister := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistDest)
	_ = cmdRegister.Run() // Ignore error if already bootstrapped

	// 4. Kickstart (start) - use -k to restart if already running
	cmdStart := exec.Command("launchctl", "kickstart", "-k", fullLabel)
	if err := cmdStart.Run(); err != nil {
		fmt.Printf("Error starting tray: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Tray icon enabled, registered, and started.")
}

func runTrayDisable() {
	userHome, _ := os.UserHomeDir()
	uid := os.Getuid()
	plistPath := fmt.Sprintf("%s/Library/LaunchAgents/com.bez.network-router.tray.plist", userHome)
	label := "gui/%d/com.bez.network-router.tray"
	fullLabel := fmt.Sprintf(label, uid)

	fmt.Printf("Disabling tray icon for user %d...\n", uid)

	// 1. Bootout (stop and unregister)
	cmdStop := exec.Command("launchctl", "bootout", fullLabel)
	_ = cmdStop.Run() // Best effort

	// 2. Remove the plist file to avoid triggering macOS background alerts
	if _, err := os.Stat(plistPath); err == nil {
		_ = os.Remove(plistPath)
		fmt.Println("✓ Removed background agent configuration.")
	}

	fmt.Println("✓ Tray icon disabled and cleaned up.")
}

func printUsage() {
	fmt.Println("Network Router CLI - Automatic Network Routing Daemon")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  network-router <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  daemon [options]    Start the routing daemon")
	fmt.Println("    -config string      Path to config file (default: config.yaml)")
	fmt.Println()
	fmt.Println("  tray                Start the system tray application")
	fmt.Println()
	fmt.Println("  status              Show daemon status")
	fmt.Println("  enable              Enable auto-routing")
	fmt.Println("  disable             Disable auto-routing")
	fmt.Println("  apply               Force apply routes now")
	fmt.Println("  clear               Force clear routes now")
	fmt.Println("  restart             Clear and re-apply routes")
	fmt.Println("  enable-dns          Enable DNS Proxy")
	fmt.Println("  disable-dns         Disable DNS Proxy")
	fmt.Println("  tray-enable         Register and start the tray icon")
	fmt.Println("  tray-disable        Stop and unregister the tray icon")
	fmt.Println()
	fmt.Println("  help                Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  sudo ./network-router daemon")
	fmt.Println("  ./network-router status")
	fmt.Println("  ./network-router enable")
	fmt.Println("  ./network-router apply")
	fmt.Println()
}
