// Package pagination provides a generic cursor-based pagination implementation.
// It handles direction detection, n+1 pattern processing, and cursor building
// for stores that need paginated list operations.
package pagination

import (
	"context"
	"slices"
)

// Direction indicates whether pagination is moving forward or backward.
type Direction int

const (
	Forward Direction = iota
	Backward
)

// Cursor groups encode/decode functions for cursor serialization.
// These are paired together to support versioning and backward compatibility.
type Cursor[T any] struct {
	Encode func(T) string               // item → cursor string
	Decode func(string) (string, error) // cursor string → position for query
}

// Config contains all parameters for a paginated query.
type Config[T any] struct {
	Limit int
	Order string // "asc" or "desc" - user's requested order
	Next  string // next cursor (empty if none)
	Prev  string // prev cursor (empty if none)

	Fetch  func(context.Context, QueryInput) ([]T, error)
	Cursor Cursor[T]
}

// QueryInput is passed to the Fetch function with computed query parameters.
type QueryInput struct {
	Limit     int
	Compare   string // "<" or ">" - cursor comparison operator
	SortDir   string // "asc" or "desc" - query sort direction (may differ from user's Order)
	CursorPos string // decoded cursor position (empty if first page)
}

// Result contains the paginated results and cursors.
type Result[T any] struct {
	Items []T
	Next  string // empty if no more pages ahead
	Prev  string // empty if no more pages behind (or first page)
}

// Run executes a paginated query.
func Run[T any](ctx context.Context, cfg Config[T]) (*Result[T], error) {
	// 1. Determine direction
	direction := determineDirection(cfg.Prev)
	isFirstPage := cfg.Next == "" && cfg.Prev == ""

	// 2. Decode cursor position
	var cursorPos string
	if cfg.Next != "" {
		pos, err := cfg.Cursor.Decode(cfg.Next)
		if err != nil {
			return nil, err
		}
		cursorPos = pos
	} else if cfg.Prev != "" {
		pos, err := cfg.Cursor.Decode(cfg.Prev)
		if err != nil {
			return nil, err
		}
		cursorPos = pos
	}

	// 3. Compute query parameters (XOR logic)
	isDesc := cfg.Order == "desc"
	isBackward := direction == Backward

	// XOR: backward flips both comparison and sort
	compare := "<"
	if isDesc != isBackward {
		compare = ">"
	}
	sortDir := cfg.Order
	if isBackward {
		sortDir = flip(cfg.Order)
	}

	// 4. Fetch with n+1
	items, err := cfg.Fetch(ctx, QueryInput{
		Limit:     cfg.Limit + 1,
		Compare:   compare,
		SortDir:   sortDir,
		CursorPos: cursorPos,
	})
	if err != nil {
		return nil, err
	}

	// 5. Process n+1 results
	hasMore := len(items) > cfg.Limit
	if hasMore {
		items = items[:cfg.Limit]
	}

	// 6. Reverse if backward
	if isBackward {
		slices.Reverse(items)
	}

	// 7. Build cursors
	var next, prev string
	if len(items) > 0 {
		firstEncoded := cfg.Cursor.Encode(items[0])
		lastEncoded := cfg.Cursor.Encode(items[len(items)-1])

		switch {
		case isFirstPage:
			if hasMore {
				next = lastEncoded
			}
		case direction == Forward:
			prev = firstEncoded
			if hasMore {
				next = lastEncoded
			}
		case direction == Backward:
			next = lastEncoded
			if hasMore {
				prev = firstEncoded
			}
		}
	}

	return &Result[T]{Items: items, Next: next, Prev: prev}, nil
}

// determineDirection returns the pagination direction based on cursors.
func determineDirection(prev string) Direction {
	if prev != "" {
		return Backward
	}
	return Forward
}

// flip reverses the sort direction.
func flip(order string) string {
	if order == "asc" {
		return "desc"
	}
	return "asc"
}
