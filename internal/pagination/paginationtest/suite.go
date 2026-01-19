// Package paginationtest provides a reusable test suite for cursor-based pagination.
package paginationtest

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ListOpts contains options for a paginated list request.
type ListOpts struct {
	Limit int
	Order string // "asc" or "desc"
	Next  string
	Prev  string
}

// ListResult contains the result of a paginated list request.
type ListResult[T any] struct {
	Items []T
	Next  string
	Prev  string
}

// Suite is a reusable test suite for cursor-based pagination.
//
// # Basic Usage
//
// For testing pagination without filters, provide NewItem and InsertMany:
//
//	suite := paginationtest.Suite[*Event]{
//	    Name:       "pglogstore",
//	    NewItem:    func(i int) *Event { ... },
//	    InsertMany: func(ctx context.Context, items []*Event) error { ... },
//	    List:       func(ctx context.Context, opts ListOpts) (ListResult[*Event], error) { ... },
//	    GetID:      func(e *Event) string { return e.ID },
//	}
//	suite.Run(t)
//
// # Testing with Filters
//
// When testing pagination with filters, use Matches to tell the suite which
// created items should appear in results:
//
//	suite := paginationtest.Suite[*Event]{
//	    NewItem: func(i int) *Event {
//	        typ := "order.created"
//	        if i%2 == 0 { typ = "order.updated" }
//	        return &Event{Type: typ, ...}
//	    },
//	    List: func(ctx context.Context, opts ListOpts) (ListResult[*Event], error) {
//	        return store.ListEvents(ctx, ListEventsRequest{EventType: "order.created", ...})
//	    },
//	    Matches: func(e *Event) bool {
//	        return e.Type == "order.created"  // mirrors filter in List
//	    },
//	}
//
// # Async/Eventually Consistent Stores
//
// For stores with async writes, use AfterInsert:
//
//	suite := paginationtest.Suite[*Event]{
//	    AfterInsert: func(ctx context.Context) error {
//	        time.Sleep(100 * time.Millisecond)
//	        return nil
//	    },
//	}
type Suite[T any] struct {
	Name        string                                     // Store name for test output
	NewItem     func(index int) T                          // Create item at index (0=oldest)
	InsertMany  func(ctx context.Context, items []T) error // Batch insert
	List        func(ctx context.Context, opts ListOpts) (ListResult[T], error)
	GetID       func(T) string                  // Unique ID for comparison
	Matches     func(T) bool                    // Filter predicate (optional)
	AfterInsert func(ctx context.Context) error // Flush hook (optional)
	Cleanup     func(ctx context.Context) error // Reset state (optional)
}

// Run executes all pagination test cases.
func (s Suite[T]) Run(t *testing.T) {
	t.Helper()

	t.Run("ForwardTraversal", s.testForwardTraversal)
	t.Run("BackwardTraversal", s.testBackwardTraversal)
	t.Run("RoundTrip", s.testRoundTrip)
	t.Run("FirstPageNoPrev", s.testFirstPageNoPrev)
	t.Run("LastPageNoNext", s.testLastPageNoNext)
	t.Run("EmptyResults", s.testEmptyResults)
	t.Run("PartialLastPage", s.testPartialLastPage)
	t.Run("ExactPageBoundary", s.testExactPageBoundary)
	t.Run("SingleItem", s.testSingleItem)
	t.Run("OrderAsc", s.testOrderAsc)
	t.Run("OrderDesc", s.testOrderDesc)
}

// setup creates items and returns the expected items after filtering.
func (s Suite[T]) setup(t *testing.T, ctx context.Context, count int) []T {
	t.Helper()

	if s.Cleanup != nil {
		require.NoError(t, s.Cleanup(ctx))
	}

	items := make([]T, count)
	for i := range count {
		items[i] = s.NewItem(i)
	}

	if count > 0 {
		require.NoError(t, s.InsertMany(ctx, items))
	}

	if s.AfterInsert != nil {
		require.NoError(t, s.AfterInsert(ctx))
	}

	// Filter to expected items
	if s.Matches != nil {
		var expected []T
		for _, item := range items {
			if s.Matches(item) {
				expected = append(expected, item)
			}
		}
		return expected
	}
	return items
}

// testForwardTraversal verifies forward pagination traverses all items without overlap.
func (s Suite[T]) testForwardTraversal(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 10)

	var collected []T
	var seenIDs = make(map[string]bool)
	pageSize := 3

	// Page 1
	res, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc"})
	require.NoError(t, err)
	assert.Empty(t, res.Prev, "first page should have no prev cursor")
	collected = append(collected, res.Items...)
	for _, item := range res.Items {
		id := s.GetID(item)
		assert.False(t, seenIDs[id], "duplicate item ID: %s", id)
		seenIDs[id] = true
	}

	// Continue until no more pages
	for res.Next != "" {
		res, err = s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Next: res.Next})
		require.NoError(t, err)
		collected = append(collected, res.Items...)
		for _, item := range res.Items {
			id := s.GetID(item)
			assert.False(t, seenIDs[id], "duplicate item ID: %s", id)
			seenIDs[id] = true
		}
	}

	assert.Len(t, collected, len(expected), "forward traversal should collect all items")
}

// testBackwardTraversal verifies backward pagination returns to previous pages correctly.
func (s Suite[T]) testBackwardTraversal(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 9)
	if len(expected) < 3 {
		t.Skip("need at least 3 items for backward traversal test")
	}

	pageSize := 3

	// Navigate forward to collect pages
	var pages []ListResult[T]
	res, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc"})
	require.NoError(t, err)
	pages = append(pages, res)

	for res.Next != "" {
		res, err = s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Next: res.Next})
		require.NoError(t, err)
		pages = append(pages, res)
	}

	if len(pages) < 2 {
		t.Skip("need at least 2 pages for backward traversal test")
	}

	// Navigate backward from the last page
	lastPage := pages[len(pages)-1]
	var backPages []ListResult[T]

	res = lastPage
	for res.Prev != "" {
		res, err = s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Prev: res.Prev})
		require.NoError(t, err)
		backPages = append(backPages, res)
	}

	// Verify backward pages match forward pages (in reverse order)
	for i, backPage := range backPages {
		forwardIdx := len(pages) - 2 - i // -2 because we started from last page
		if forwardIdx < 0 {
			break
		}
		forwardPage := pages[forwardIdx]
		require.Len(t, backPage.Items, len(forwardPage.Items), "page %d item count mismatch", i)
		for j, item := range backPage.Items {
			assert.Equal(t, s.GetID(forwardPage.Items[j]), s.GetID(item),
				"backward page %d item %d mismatch", i, j)
		}
	}

	// Verify we're back at first page (no prev cursor)
	if len(backPages) > 0 {
		assert.Empty(t, backPages[len(backPages)-1].Prev, "back at first page should have no prev")
	}
}

// testRoundTrip verifies forward then backward returns to same data.
func (s Suite[T]) testRoundTrip(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 9)
	if len(expected) < 6 {
		t.Skip("need at least 6 items for round trip test")
	}

	pageSize := 3

	// Page 1
	page1, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc"})
	require.NoError(t, err)

	if page1.Next == "" {
		t.Skip("only one page, cannot test round trip")
	}

	// Page 2
	page2, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Next: page1.Next})
	require.NoError(t, err)

	// Back to page 1
	back1, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Prev: page2.Prev})
	require.NoError(t, err)

	// Verify back1 matches page1
	require.Len(t, back1.Items, len(page1.Items))
	for i, item := range back1.Items {
		assert.Equal(t, s.GetID(page1.Items[i]), s.GetID(item),
			"round trip item %d mismatch", i)
	}
}

// testFirstPageNoPrev verifies first page must never have a prev cursor.
func (s Suite[T]) testFirstPageNoPrev(t *testing.T) {
	ctx := context.Background()
	s.setup(t, ctx, 5)

	res, err := s.List(ctx, ListOpts{Limit: 3, Order: "desc"})
	require.NoError(t, err)
	assert.Empty(t, res.Prev, "first page should have no prev cursor")
}

// testLastPageNoNext verifies last page must never have a next cursor.
func (s Suite[T]) testLastPageNoNext(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 5)
	if len(expected) == 0 {
		t.Skip("no items to test")
	}

	pageSize := 3

	// Navigate to last page
	res, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc"})
	require.NoError(t, err)

	for res.Next != "" {
		res, err = s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Next: res.Next})
		require.NoError(t, err)
	}

	assert.Empty(t, res.Next, "last page should have no next cursor")
	if len(expected) > pageSize {
		assert.NotEmpty(t, res.Prev, "last page should have prev cursor when more data exists")
	}
}

// testEmptyResults verifies empty result set has no cursors.
func (s Suite[T]) testEmptyResults(t *testing.T) {
	ctx := context.Background()
	s.setup(t, ctx, 0)

	res, err := s.List(ctx, ListOpts{Limit: 10, Order: "desc"})
	require.NoError(t, err)
	assert.Empty(t, res.Items, "should have no items")
	assert.Empty(t, res.Next, "should have no next cursor")
	assert.Empty(t, res.Prev, "should have no prev cursor")
}

// testPartialLastPage verifies last page may have fewer items than limit.
func (s Suite[T]) testPartialLastPage(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 7)
	if len(expected) == 0 {
		t.Skip("no items to test")
	}

	pageSize := 3 // 7 items / 3 = 2 full pages + 1 partial page with 1 item

	// Navigate to last page
	res, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc"})
	require.NoError(t, err)

	for res.Next != "" {
		res, err = s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Next: res.Next})
		require.NoError(t, err)
	}

	expectedLastPageSize := len(expected) % pageSize
	if expectedLastPageSize == 0 {
		expectedLastPageSize = pageSize
	}
	assert.Len(t, res.Items, expectedLastPageSize, "last page should have %d items", expectedLastPageSize)
	assert.Empty(t, res.Next, "last page should have no next cursor")
}

// testExactPageBoundary verifies when items divide evenly by limit, last page is full but has no next.
func (s Suite[T]) testExactPageBoundary(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 6)
	if len(expected) != 6 {
		t.Skip("need exactly 6 items for exact page boundary test")
	}

	pageSize := 3 // exactly 2 pages

	// Page 1
	page1, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc"})
	require.NoError(t, err)
	assert.Len(t, page1.Items, pageSize)
	assert.NotEmpty(t, page1.Next, "page 1 should have next cursor")

	// Page 2
	page2, err := s.List(ctx, ListOpts{Limit: pageSize, Order: "desc", Next: page1.Next})
	require.NoError(t, err)
	assert.Len(t, page2.Items, pageSize)
	assert.Empty(t, page2.Next, "page 2 should have no next cursor (last page)")
}

// testSingleItem verifies single item has no pagination cursors.
func (s Suite[T]) testSingleItem(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 1)
	if len(expected) != 1 {
		t.Skip("need exactly 1 item for single item test")
	}

	res, err := s.List(ctx, ListOpts{Limit: 10, Order: "desc"})
	require.NoError(t, err)
	assert.Len(t, res.Items, 1, "should have 1 item")
	assert.Empty(t, res.Next, "should have no next cursor")
	assert.Empty(t, res.Prev, "should have no prev cursor")
}

// testOrderAsc verifies ascending order returns oldest items first.
func (s Suite[T]) testOrderAsc(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 5)
	if len(expected) == 0 {
		t.Skip("no items to test")
	}

	res, err := s.List(ctx, ListOpts{Limit: 10, Order: "asc"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Items)

	// First item should be the oldest (index 0)
	assert.Equal(t, s.GetID(expected[0]), s.GetID(res.Items[0]),
		"first item in asc order should be oldest")

	// Verify ascending order
	for i := 1; i < len(res.Items); i++ {
		// Items should be in same order as expected (which is created in index order)
		assert.Equal(t, s.GetID(expected[i]), s.GetID(res.Items[i]),
			"item %d should match expected order", i)
	}
}

// testOrderDesc verifies descending order returns newest items first.
func (s Suite[T]) testOrderDesc(t *testing.T) {
	ctx := context.Background()
	expected := s.setup(t, ctx, 5)
	if len(expected) == 0 {
		t.Skip("no items to test")
	}

	res, err := s.List(ctx, ListOpts{Limit: 10, Order: "desc"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Items)

	// First item should be the newest (last index)
	assert.Equal(t, s.GetID(expected[len(expected)-1]), s.GetID(res.Items[0]),
		"first item in desc order should be newest")

	// Verify descending order (reverse of expected)
	reversed := make([]T, len(expected))
	copy(reversed, expected)
	slices.Reverse(reversed)
	for i := 0; i < len(res.Items); i++ {
		assert.Equal(t, s.GetID(reversed[i]), s.GetID(res.Items[i]),
			"item %d should match expected desc order", i)
	}
}
