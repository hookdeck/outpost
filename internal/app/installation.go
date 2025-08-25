package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/telemetry"
)

const (
	outpostrcKey    = "outpostrc"
	installationKey = "installation"
)

func getInstallation(ctx context.Context, redisClient redis.Cmdable, telemetryConfig telemetry.TelemetryConfig) (string, error) {
	if telemetryConfig.Disabled {
		return "", nil
	}

	// First attempt: try to get existing installation ID
	installationID, err := redisClient.HGet(ctx, outpostrcKey, installationKey).Result()
	if err == nil {
		// Installation ID already exists
		return installationID, nil
	}

	if err != redis.Nil {
		// Unexpected error
		return "", err
	}

	// Installation ID doesn't exist, create one atomically
	newInstallationID := uuid.New().String()

	// Use HSETNX to atomically set the installation ID only if it doesn't exist
	// This prevents race conditions when multiple Outpost instances start simultaneously
	wasSet, err := redisClient.HSetNX(ctx, outpostrcKey, installationKey, newInstallationID).Result()
	if err != nil {
		return "", err
	}

	if wasSet {
		// We successfully set the installation ID
		return newInstallationID, nil
	}

	// Another instance set the installation ID while we were generating ours
	// Fetch the installation ID that was actually set
	installationID, err = redisClient.HGet(ctx, outpostrcKey, installationKey).Result()
	if err != nil {
		return "", err
	}

	return installationID, nil
}
