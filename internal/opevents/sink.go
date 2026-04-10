package opevents

import "context"

// Sink is the interface for delivering operation events to an external system.
type Sink interface {
	Init(ctx context.Context) error
	Send(ctx context.Context, event *OperationEvent) error
	Close() error
}
