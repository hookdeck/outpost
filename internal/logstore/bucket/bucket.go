package bucket

import (
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// TruncateTime truncates t to the boundary defined by granularity g.
// This is the shared implementation used by all backends.
func TruncateTime(t time.Time, g *driver.Granularity) time.Time {
	t = t.UTC()
	switch g.Unit {
	case "s":
		d := time.Duration(g.Value) * time.Second
		return t.Truncate(d)
	case "m":
		d := time.Duration(g.Value) * time.Minute
		return t.Truncate(d)
	case "h":
		d := time.Duration(g.Value) * time.Hour
		return t.Truncate(d)
	case "d":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	case "w":
		weekday := int(t.Weekday())
		return time.Date(t.Year(), t.Month(), t.Day()-weekday, 0, 0, 0, 0, time.UTC)
	case "M":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return t
	}
}

// AdvanceTime steps forward by one granularity unit from t.
func AdvanceTime(t time.Time, g *driver.Granularity) time.Time {
	switch g.Unit {
	case "s":
		return t.Add(time.Duration(g.Value) * time.Second)
	case "m":
		return t.Add(time.Duration(g.Value) * time.Minute)
	case "h":
		return t.Add(time.Duration(g.Value) * time.Hour)
	case "d":
		return t.AddDate(0, 0, g.Value)
	case "w":
		return t.AddDate(0, 0, 7*g.Value)
	case "M":
		return t.AddDate(0, g.Value, 0)
	default:
		return t
	}
}

// GenerateTimeBuckets returns a slice of aligned time slots from the truncated
// start up to (but not including) end, stepping by one granularity unit.
func GenerateTimeBuckets(start, end time.Time, g *driver.Granularity) []time.Time {
	cur := TruncateTime(start, g)
	var buckets []time.Time
	for cur.Before(end) {
		buckets = append(buckets, cur)
		cur = AdvanceTime(cur, g)
	}
	return buckets
}
