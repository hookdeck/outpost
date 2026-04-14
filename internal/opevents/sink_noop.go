package opevents

import "context"

// NoopSink is a sink that discards all events. Used when no sink is configured.
type NoopSink struct{}

func (s *NoopSink) Init(ctx context.Context) error                       { return nil }
func (s *NoopSink) Send(ctx context.Context, event *OperatorEvent) error { return nil }
func (s *NoopSink) Close() error                                         { return nil }
