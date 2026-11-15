// Copyright 2026 bez
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

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

	var err error
	switch command {
	case "daemon":
		runDaemon()
	case "tray":
		runTray()
	case "tray-enable":
		runTrayEnable()
	case "tray-disable":
		runTrayDisable()
	case "status":
		err = client.NewClient().Status()
	case "help", "-h", "--help":
		printUsage()
	default:
		action, ok := client.Commands[command]
		if !ok {
			fmt.Printf("Unknown command: %s\n\n", command)
			printUsage()
			os.Exit(1)
		}
		err = client.NewClient().Run(action)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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

	// 3. Unload any registered job. A job whose binary was missing at respawn
	// stays in "spawn failed" and is never retried; kickstart on it waits
	// forever. A fresh bootstrap clears that state.
	_ = exec.Command("launchctl", "bootout", fullLabel).Run()
	for i := 0; i < 10 && exec.Command("launchctl", "print", fullLabel).Run() == nil; i++ {
		time.Sleep(500 * time.Millisecond)
	}

	// 4. Bootstrap (register)
	if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistDest).CombinedOutput(); err != nil {
		fmt.Printf("Error registering tray: %v: %s\n", err, out)
		os.Exit(1)
	}

	// 5. Kickstart (start). The time limit keeps a stuck launchd job from
	// hanging this command.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "launchctl", "kickstart", fullLabel).CombinedOutput(); err != nil {
		fmt.Printf("Error starting tray: %v: %s\n", err, out)
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
	fmt.Println("  daemon [options]      Start the routing daemon")
	fmt.Println("    -config string        Path to config file (default: config.yaml)")
	fmt.Println()
	fmt.Println("  tray                  Start the system tray application")
	fmt.Println()
	fmt.Println("  status                Show daemon status")
	fmt.Println("  on                    Turn routing on; apply routes when tethering is ready")
	fmt.Println("                        (aliases: enable, apply, restart)")
	fmt.Println("  off                   Turn routing off and clear routes (aliases: disable, clear)")
	fmt.Println("  refresh               Re-resolve domains and re-apply routes")
	fmt.Println("  tray-enable           Register and start the tray icon")
	fmt.Println("  tray-disable          Stop and unregister the tray icon")
	fmt.Println()
	fmt.Println("  help                  Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  sudo ./network-router daemon")
	fmt.Println("  ./network-router status")
	fmt.Println("  ./network-router on")
	fmt.Println()
}
