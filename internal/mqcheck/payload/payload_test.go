package payload_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqcheck/payload"
)

func gzipRatio(t *testing.T, b []byte) float64 {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(b)
	w.Close()
	return float64(len(b)) / float64(buf.Len())
}

func TestBody(t *testing.T) {
	g := payload.NewGenerator(1)
	for _, tc := range []struct {
		spec               string
		minRatio, maxRatio float64
	}{
		{"json-2KB", 1.5, 8},
		{"json-6KB", 2, 8},
		{"json-1MB", 2, 8},
		{"rand-64KB", 1, 1.4},
		{"rand-1MB", 1, 1.4},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			spec, err := payload.ParseSpec(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			a := g.Body("m1", spec, payload.DoNackOnce)
			b := g.Body("m2", spec, "")
			if bytes.Equal(a, b) {
				t.Fatal("two bodies are identical")
			}
			if diff := float64(len(a)-spec.Size) / float64(spec.Size); diff < -0.05 || diff > 0.05 {
				t.Errorf("size %d, want %d ± 5%%", len(a), spec.Size)
			}
			if r := gzipRatio(t, a); r < tc.minRatio || r > tc.maxRatio {
				t.Errorf("gzip ratio %.2f, want %.1f-%.1f", r, tc.minRatio, tc.maxRatio)
			}
			if bytes.Contains(a, bytes.Repeat([]byte{'x'}, 32)) {
				t.Error("body has repeated-character padding")
			}
			var task models.DeliveryTask
			if err := json.Unmarshal(a, &task); err != nil {
				t.Fatalf("not a delivery task: %v", err)
			}
			if task.Event.ID != "m1" || task.Event.Metadata[payload.MetaDirective] != payload.DoNackOnce {
				t.Errorf("id/directive not set: %q %q", task.Event.ID, task.Event.Metadata[payload.MetaDirective])
			}
		})
	}
}

func TestStamp(t *testing.T) {
	spec, _ := payload.ParseSpec("json-2KB")
	b := payload.NewGenerator(2).Body("m1", spec, "")
	now := time.Now()
	payload.Stamp(b, now)
	var task models.DeliveryTask
	if err := json.Unmarshal(b, &task); err != nil {
		t.Fatal(err)
	}
	got, _ := strconv.ParseInt(task.Event.Metadata[payload.MetaPublished], 10, 64)
	if got != now.UnixNano() {
		t.Errorf("stamped %d, want %d", got, now.UnixNano())
	}
}

func TestParseBytes(t *testing.T) {
	for in, want := range map[string]int64{"512B": 512, "6KB": 6 << 10, "1MB": 1 << 20, "100MiB": 100 << 20, "4096": 4096} {
		if got, err := payload.ParseBytes(in); err != nil || got != want {
			t.Errorf("ParseBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := payload.ParseSpec("xml-1KB"); err == nil {
		t.Error("ParseSpec accepted an unknown kind")
	}
}
