package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// printConfigList renders the output for `outpost config list`: one
// "key  value" line per field, sorted by key for a stable, diffable output.
func printConfigList(w io.Writer, fields []zap.Field) {
	sorted := make([]zap.Field, len(fields))
	copy(sorted, fields)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	maxKeyLen := 0
	for _, f := range sorted {
		if len(f.Key) > maxKeyLen {
			maxKeyLen = len(f.Key)
		}
	}

	for _, f := range sorted {
		fmt.Fprintf(w, "%-*s  %s\n", maxKeyLen, f.Key, formatFieldValue(f))
	}
}

// formatFieldValue renders a zap.Field's value as plain text. It covers the
// field types LogConfigurationSummary actually produces — bool/string/int64
// via zap.Bool/String/Int, plus zap.Strings/zap.Ints arrays. Anything else
// falls back to fmt.Sprint on the boxed value, which is sufficient for
// display purposes.
func formatFieldValue(f zap.Field) string {
	switch f.Type {
	case zapcore.BoolType:
		return strconv.FormatBool(f.Integer == 1)
	case zapcore.StringType:
		return f.String
	case zapcore.Int64Type:
		return strconv.FormatInt(f.Integer, 10)
	default:
		return fmt.Sprint(f.Interface)
	}
}
