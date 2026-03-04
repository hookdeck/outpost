package memlogstore

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

var errNotImplemented = errors.New("metrics queries not yet implemented")

func (s *memLogStore) QueryEventMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.EventMetricsResponse, error) {
	return nil, errNotImplemented
}

func (s *memLogStore) QueryAttemptMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.AttemptMetricsResponse, error) {
	return nil, errNotImplemented
}
