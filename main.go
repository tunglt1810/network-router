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
	"network-router/client"
	"network-router/daemon"
	"network-router/tray"
	"os"
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
	fmt.Println("  help                Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  sudo ./network-router daemon")
	fmt.Println("  ./network-router status")
	fmt.Println("  ./network-router enable")
	fmt.Println("  ./network-router apply")
	fmt.Println()
}
