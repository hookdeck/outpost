package mcpevents

import "github.com/hookdeck/outpost/internal/models"

// ArgumentsToFilter maps subscription arguments to the destination filter.
// Arguments target event data, so the result is wrapped in {"data": ...}; per
// argument, a scalar matches as is ($eq), a list becomes {"$in": list} and an
// operator object ($gt, $gte, $lt, $lte) passes through. Empty arguments mean
// no filter (nil).
//
// args must already have passed the topic's inputSchema, which only offers
// payload properties (never "$"-prefixed names) and those three shapes. The
// result shares nothing with args.
func ArgumentsToFilter(args map[string]any) models.Filter {
	if len(args) == 0 {
		return nil
	}
	data := make(map[string]any, len(args))
	for k, v := range args {
		if list, ok := v.([]any); ok {
			data[k] = map[string]any{"$in": deepCopy(list)}
			continue
		}
		data[k] = deepCopy(v)
	}
	return models.Filter{"data": data}
}

func deepCopy(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, e := range v {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		s := make([]any, len(v))
		for i, e := range v {
			s[i] = deepCopy(e)
		}
		return s
	default:
		return v
	}
}
