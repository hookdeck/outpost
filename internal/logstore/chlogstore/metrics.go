package chlogstore

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

var errNotImplemented = errors.New("metrics queries not yet implemented")

func (s *logStoreImpl) QueryEventMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.EventMetricsResponse, error) {
	return nil, errNotImplemented
}

func (s *logStoreImpl) QueryAttemptMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.AttemptMetricsResponse, error) {
	return nil, errNotImplemented
}
