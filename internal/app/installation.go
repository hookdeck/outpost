package app

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/telemetry"
)

const (
	installationIDKey = "outpost:installation_id"
)

func installationKey(deploymentID string) string {
	if deploymentID == "" {
		return installationIDKey
	}
	return fmt.Sprintf("%s:%s", deploymentID, installationIDKey)
}

func getInstallation(ctx context.Context, redisClient redis.Cmdable, telemetryConfig telemetry.TelemetryConfig, deploymentID string) (string, error) {
	if telemetryConfig.Disabled {
		return "", nil
	}

	key := installationKey(deploymentID)

	// First attempt: try to get existing installation ID
	installationID, err := redisClient.Get(ctx, key).Result()
	if err == nil {
		return installationID, nil
	}

	if err != redis.Nil {
		return "", err
	}

	// Installation ID doesn't exist, create one atomically
	newInstallationID := idgen.Installation()

	// Use SetNX to atomically set the installation ID only if it doesn't exist
	// This prevents race conditions when multiple Outpost instances start simultaneously
	wasSet, err := redisClient.SetNX(ctx, key, newInstallationID, time.Duration(0)).Result()
	if err != nil {
		return "", err
	}

	if wasSet {
		return newInstallationID, nil
	}

	// Another instance set the installation ID while we were generating ours
	// Fetch the installation ID that was actually set
	installationID, err = redisClient.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}

	return installationID, nil
}
