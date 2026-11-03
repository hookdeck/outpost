package destregistry

import "context"

type messageIDKey struct{}

// WithMessageID returns a context carrying the identifier of the delivery being
// attempted.
func WithMessageID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, messageIDKey{}, id)
}

// MessageIDFrom returns the identifier set by WithMessageID, or "".
func MessageIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(messageIDKey{}).(string)
	return id
}
