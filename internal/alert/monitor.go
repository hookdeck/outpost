package alert

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type alertMonitor struct {
	store                   AlertStore
	evaluator               AlertEvaluator
	notifier                AlertNotifier
	autoDisableFailureCount int
}

// NewAlertMonitor creates a new alert monitor with default implementations
func NewAlertMonitor(redisClient *redis.Client, config AlertConfig) AlertMonitor {
	store := NewRedisAlertStore(redisClient)
	evaluator := NewAlertEvaluator(config)
	notifier := NewHTTPAlertNotifier(config.CallbackURL)

	return &alertMonitor{
		store:                   store,
		evaluator:               evaluator,
		notifier:                notifier,
		autoDisableFailureCount: config.AutoDisableFailureCount,
	}
}

func (m *alertMonitor) HandleAttempt(ctx context.Context, attempt DeliveryAttempt) error {
	if attempt.Success {
		return m.store.ResetFailures(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	}

	return m.store.WithTx(ctx, func(tx AlertStore) error {
		// Increment failures
		failures, err := tx.IncrementFailures(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
		if err != nil {
			return fmt.Errorf("failed to increment failures: %w", err)
		}

		// Get last alert time
		lastAlertTime, err := tx.GetLastAlertTime(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
		if err != nil && err != ErrNotFound {
			return fmt.Errorf("failed to get last alert time: %w", err)
		}

		// Check if we should send an alert
		if !m.evaluator.ShouldAlert(failures, lastAlertTime) {
			return nil
		}

		// Get alert level
		level, _ := m.evaluator.GetAlertLevel(failures)

		// Create and send alert
		alert := Alert{
			Topic:               "event.failed",
			DisableThreshold:    m.autoDisableFailureCount,
			ConsecutiveFailures: failures,
			Destination:         attempt.Destination,
			Response:            attempt.Response,
		}

		if err := m.notifier.Notify(ctx, alert); err != nil {
			return fmt.Errorf("failed to send alert: %w", err)
		}

		// Update last alert time
		if err := tx.UpdateLastAlertTime(ctx, attempt.Destination.TenantID, attempt.Destination.ID, attempt.Timestamp); err != nil {
			return fmt.Errorf("failed to update last alert time: %w", err)
		}

		// If we've hit 100%, we should disable the destination
		if level == 100 {
			// TODO: Call destination service to disable the destination
		}

		return nil
	})
}
