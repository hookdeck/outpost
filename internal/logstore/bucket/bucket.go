package bucket

import (
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

const maxBuckets = 100000

// ErrTooManyBuckets is returned when the granularity + time range would
// produce more than maxBuckets time slots, which could cause OOM.
var ErrTooManyBuckets = fmt.Errorf("time range produces more than %d buckets", maxBuckets)

// epochDay is the anchor for epoch-based day/week alignment (1970-01-01 UTC).
var epochDay = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

// epochSunday is the first Sunday on or after the Unix epoch (1970-01-04 UTC),
// used as the anchor for Sunday-based week alignment.
var epochSunday = time.Date(1970, 1, 4, 0, 0, 0, 0, time.UTC)

// TruncateTime truncates t to the boundary defined by granularity g.
// This is the shared implementation used by all backends.
//
// For sub-day units (s, m, h), Value controls both step size and alignment.
// For calendar units (d, w, M), Value > 1 uses epoch-anchored alignment so
// that multi-day/week/month intervals aggregate data correctly.
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
		dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		if g.Value == 1 {
			return dayStart
		}
		days := int(dayStart.Sub(epochDay).Hours() / 24)
		aligned := (days / g.Value) * g.Value
		return epochDay.AddDate(0, 0, aligned)
	case "w":
		weekday := int(t.Weekday())
		sunday := time.Date(t.Year(), t.Month(), t.Day()-weekday, 0, 0, 0, 0, time.UTC)
		if g.Value == 1 {
			return sunday
		}
		weeks := int(sunday.Sub(epochSunday).Hours() / (7 * 24))
		aligned := (weeks / g.Value) * g.Value
		return epochSunday.AddDate(0, 0, aligned*7)
	case "M":
		if g.Value == 1 {
			return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
		totalMonths := (t.Year()-1970)*12 + int(t.Month()-1)
		aligned := (totalMonths / g.Value) * g.Value
		y := 1970 + aligned/12
		m := time.Month(aligned%12 + 1)
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
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
// Returns ErrTooManyBuckets if the result would exceed maxBuckets.
func GenerateTimeBuckets(start, end time.Time, g *driver.Granularity) ([]time.Time, error) {
	cur := TruncateTime(start, g)
	var buckets []time.Time
	for cur.Before(end) {
		if len(buckets) >= maxBuckets {
			return nil, ErrTooManyBuckets
		}
		buckets = append(buckets, cur)
		next := AdvanceTime(cur, g)
		if !next.After(cur) {
			break // safety: prevent infinite loop if time doesn't advance
		}
		cur = next
	}
	return buckets, nil
}
