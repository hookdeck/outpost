package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/loadtest/mock/webhook/server"
)

func main() {
	// Default configuration
	port := "8080"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}

	// Parse delay mode configuration from environment
	delayMode := false
	if envDelayMode := os.Getenv("DELAY_MODE"); envDelayMode == "true" || envDelayMode == "1" {
		delayMode = true
	}

	minDelay := 1 * time.Second
	if envMinDelay := os.Getenv("MIN_DELAY"); envMinDelay != "" {
		if d, err := time.ParseDuration(envMinDelay); err == nil {
			minDelay = d
		}
	}

	maxDelay := 2 * time.Second
	if envMaxDelay := os.Getenv("MAX_DELAY"); envMaxDelay != "" {
		if d, err := time.ParseDuration(envMaxDelay); err == nil {
			maxDelay = d
		}
	}

	slowDelayMin := 30 * time.Second
	if envSlowDelayMin := os.Getenv("SLOW_DELAY_MIN"); envSlowDelayMin != "" {
		if d, err := time.ParseDuration(envSlowDelayMin); err == nil {
			slowDelayMin = d
		}
	}

	slowDelayMax := 35 * time.Second
	if envSlowDelayMax := os.Getenv("SLOW_DELAY_MAX"); envSlowDelayMax != "" {
		if d, err := time.ParseDuration(envSlowDelayMax); err == nil {
			slowDelayMax = d
		}
	}

	slowPercent := 0.1 // Default 0.1%
	if envSlowPercent := os.Getenv("SLOW_PERCENT"); envSlowPercent != "" {
		if p, err := strconv.ParseFloat(envSlowPercent, 64); err == nil {
			slowPercent = p
		}
	}

	// Create the webhook server with configuration
	srv := server.NewServer(server.Config{
		EventTTL:     10 * time.Minute, // Default 10 minutes TTL for events
		MaxSize:      10000,            // Maximum number of events to store
		DelayMode:    delayMode,
		MinDelay:     minDelay,
		MaxDelay:     maxDelay,
		SlowDelayMin: slowDelayMin,
		SlowDelayMax: slowDelayMax,
		SlowPercent:  slowPercent,
	})

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: srv.Routes(),
	}

	// Channel to listen for interrupts
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		log.Printf("Mock Webhook Server starting on port %s", port)
		if delayMode {
			log.Printf("⏱️  DELAY MODE ENABLED:")
			log.Printf("   - Normal delay: %v - %v", minDelay, maxDelay)
			log.Printf("   - Slow delay: %v - %v (%.2f%% of requests)", slowDelayMin, slowDelayMax, slowPercent)
		} else {
			log.Printf("⚡ DELAY MODE DISABLED - No artificial delays")
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-stop
	log.Println("Shutting down server...")

	// Create context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}
