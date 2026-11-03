package mcpevents

import "github.com/hookdeck/outpost/internal/models"

// ArgumentsToFilter maps subscription arguments to the destination filter.
// Each argument matches the event data field of the same name by equality, so
// the result is {"data": {name: value}}. Empty arguments mean no filter (nil).
//
// args must already have passed the event's inputSchema, which only offers
// scalar values. The result shares nothing with args.
func ArgumentsToFilter(args map[string]any) models.Filter {
	if len(args) == 0 {
		return nil
	}
	data := make(map[string]any, len(args))
	for k, v := range args {
		data[k] = v
	}
	return models.Filter{"data": data}
}
