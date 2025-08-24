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

	// TODO: consider using WATCH to avoid race condition
	// There's a potential race condition when multiple Outpost instances are started at the same time.
	// However, given this is for telemetry purposes, and it will be a temporary issue, we can ignore it for now.
	installationID, err := redisClient.HGet(ctx, outpostrcKey, installationKey).Result()
	if err != nil {
		if err == redis.Nil {
			installationID = uuid.New().String()
			if err = redisClient.HSet(ctx, outpostrcKey, installationKey, installationID).Err(); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	return installationID, nil
}
