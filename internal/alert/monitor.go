package alert

import (
	"context"
	"fmt"
	"time"

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

// NewAlertMonitorWithDeps creates a monitor with the provided dependencies
func NewAlertMonitorWithDeps(store AlertStore, evaluator AlertEvaluator, notifier AlertNotifier, config AlertConfig) AlertMonitor {
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

	// Get failure state
	state, err := m.store.IncrementAndGetFailureState(ctx, attempt.Destination.TenantID, attempt.Destination.ID)
	if err != nil {
		return fmt.Errorf("failed to get failure state: %w", err)
	}

	// Check if we should send an alert
	level, shouldAlert := m.evaluator.ShouldAlert(state.FailureCount, state.LastAlertTime, state.LastAlertLevel)
	if !shouldAlert {
		return nil
	}

	// Send alert
	alert := Alert{
		Topic:               attempt.DeliveryEvent.Event.Topic,
		DisableThreshold:    m.autoDisableFailureCount,
		ConsecutiveFailures: state.FailureCount,
		Destination:         attempt.Destination,
		Response:            attempt.Response,
	}

	if err := m.notifier.Notify(ctx, alert); err != nil {
		return fmt.Errorf("failed to send alert: %w", err)
	}

	// Update last alert time and level atomically
	if err := m.store.UpdateLastAlert(ctx, attempt.Destination.TenantID, attempt.Destination.ID, time.Now(), level); err != nil {
		return fmt.Errorf("failed to update last alert state: %w", err)
	}

	// If we've hit 100%, we should disable the destination
	if level == 100 {
		// TODO: Call destination service to disable the destination
	}

	return nil
}
