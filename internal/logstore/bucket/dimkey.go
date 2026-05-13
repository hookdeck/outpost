package bucket

import (
	"fmt"
	"strings"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// DimKey is an opaque composite key built from dimension values.
type DimKey string

// EventDimKey builds a composite key from the dimension fields of an event data point.
func EventDimKey(dp *driver.EventMetricsDataPoint, dims []string) DimKey {
	parts := make([]string, len(dims))
	for i, dim := range dims {
		switch dim {
		case "tenant_id":
			parts[i] = derefStr(dp.TenantID)
		case "topic":
			parts[i] = derefStr(dp.Topic)
		case "destination_id":
			parts[i] = derefStr(dp.DestinationID)
		}
	}
	return DimKey(strings.Join(parts, "\x00"))
}

// AttemptDimKey builds a composite key from the dimension fields of an attempt data point.
func AttemptDimKey(dp *driver.AttemptMetricsDataPoint, dims []string) DimKey {
	parts := make([]string, len(dims))
	for i, dim := range dims {
		switch dim {
		case "tenant_id":
			parts[i] = derefStr(dp.TenantID)
		case "destination_id":
			parts[i] = derefStr(dp.DestinationID)
		case "destination_type":
			parts[i] = derefStr(dp.DestinationType)
		case "topic":
			parts[i] = derefStr(dp.Topic)
		case "status":
			parts[i] = derefStr(dp.Status)
		case "code":
			parts[i] = derefStr(dp.Code)
		case "manual":
			parts[i] = derefBool(dp.Manual)
		case "attempt_number":
			parts[i] = derefInt(dp.AttemptNumber)
		}
	}
	return DimKey(strings.Join(parts, "\x00"))
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%t", *p)
}

func derefInt(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}
