// Package main provides the REST API server entry point for QueryBridge.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MythicalMAxX/QueryBridge/internal/api"
	"github.com/MythicalMAxX/QueryBridge/pkg/querybridge"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "../config.json", "Path to configuration file")
	port := flag.Int("port", 8080, "HTTP server port")
	staticDir := flag.String("static", "../web", "Path to static web files")
	historyPath := flag.String("history", "./history.db", "Path to history database")
	flag.Parse()

	// Check for environment variables
	if envConfig := os.Getenv("QB_CONFIG"); envConfig != "" {
		*configPath = envConfig
	}
	if envPort := os.Getenv("QB_PORT"); envPort != "" {
		fmt.Sscanf(envPort, "%d", port)
	}

	log.Printf("QueryBridge API Server starting...")
	log.Printf("Config: %s", *configPath)
	log.Printf("Port: %d", *port)
	log.Printf("Static files: %s", *staticDir)

	// Create QueryBridge
	bridge, err := querybridge.NewBridge(*configPath)
	if err != nil {
		log.Fatalf("Failed to create QueryBridge: %v", err)
	}
	defer bridge.Close()

	log.Printf("Connected to %d database(s): %v", bridge.DatabaseCount(), bridge.DatabaseNames())

	// Create API server
	cfg := api.Config{
		Port:        *port,
		CORSOrigins: []string{"*"},
		ExportDir:   "./exports",
		HistoryPath: *historyPath,
		StaticDir:   *staticDir,
	}

	server, err := api.NewServer(bridge, cfg)
	if err != nil {
		log.Fatalf("Failed to create API server: %v", err)
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Received shutdown signal")
		
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		defer shutdownCancel()
		
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
		cancel()
	}()

	// Start server
	fmt.Println()
	fmt.Println("â•”â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•—")
	fmt.Printf("â•‘           QueryBridge REST API Server - Port %d             â•‘\n", *port)
	fmt.Println("â• â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•£")
	fmt.Printf("â•‘  Web UI:     http://localhost:%d                            â•‘\n", *port)
	fmt.Printf("â•‘  API Docs:   http://localhost:%d/api/databases              â•‘\n", *port)
	fmt.Println("â•šâ•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•â•")
	fmt.Println()

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
