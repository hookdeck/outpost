package models_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
)

func nestedObjects(depth int) models.Filter {
	filter := models.Filter{"status": "paid"}
	for i := 1; i < depth; i++ {
		filter = models.Filter{"data": map[string]any(filter)}
	}
	return filter
}

func nestedArrays(depth int) models.Filter {
	var inner any = "paid"
	for i := 1; i < depth; i++ {
		inner = []any{inner}
	}
	return models.Filter{"$or": inner}
}

func TestFilter_Validate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		filter models.Filter
		err    error
	}{
		{name: "nil", filter: nil},
		{name: "empty", filter: models.Filter{}},
		{name: "operators", filter: models.Filter{"data": map[string]any{"amount": map[string]any{"$gte": 100}}}},
		{name: "objects at the limit", filter: nestedObjects(models.MaxFilterDepth)},
		{name: "objects over the limit", filter: nestedObjects(models.MaxFilterDepth + 1), err: models.ErrFilterTooDeep},
		{name: "arrays at the limit", filter: nestedArrays(models.MaxFilterDepth)},
		{name: "arrays over the limit", filter: nestedArrays(models.MaxFilterDepth + 1), err: models.ErrFilterTooDeep},
		{name: "objects and arrays at the limit", filter: models.Filter{"$or": []any{map[string]any(nestedObjects(models.MaxFilterDepth - 2))}}},
		{name: "objects and arrays over the limit", filter: models.Filter{"$or": []any{map[string]any(nestedObjects(models.MaxFilterDepth - 1))}}, err: models.ErrFilterTooDeep},
		{name: "far over the limit", filter: nestedObjects(5000), err: models.ErrFilterTooDeep},
		{name: "deep branch beside a shallow one", filter: models.Filter{"topic": "orders", "data": map[string]any(nestedObjects(models.MaxFilterDepth))}, err: models.ErrFilterTooDeep},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.ErrorIs(t, tc.filter.Validate(), tc.err)
		})
	}
}

func TestDestination_Validate_FilterDepth(t *testing.T) {
	t.Parallel()

	destination := models.Destination{Topics: models.Topics{"*"}, Filter: nestedObjects(models.MaxFilterDepth + 1)}
	assert.ErrorIs(t, destination.Validate(nil, false), models.ErrFilterTooDeep)

	destination.Filter = nestedObjects(models.MaxFilterDepth)
	assert.NoError(t, destination.Validate(nil, false))
}
