// Package payload generates realistic delivery queue messages: valid Outpost
// delivery tasks whose event data is webhook-like nested JSON (compressible)
// or random bytes (incompressible). Every message is different. Never
// repeated-character padding: brokers that compress or count compressed size
// (Pub/Sub) would see a size that no real payload has.
package payload

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// Metadata keys the harness puts on every event.
const (
	MetaPublished = "mqcheck_pub" // publish time, unix ns, 19 digits
	MetaDirective = "mqcheck_do"  // what the synthetic handler does with it
)

// Directives for the synthetic handler, set per message.
const (
	DoNackOnce   = "nack-once"   // nack the first invocation in a process, then the default
	DoNackAlways = "nack-always" // always nack
	DoExceedOnce = "exceed-once" // first invocation runs past the visibility timeout, then the default
)

const pubPlaceholder = "0000000000000000000"

// Kind of event data.
type Kind string

const (
	KindJSON Kind = "json" // nested webhook-like JSON, gzip ratio 2-4
	KindRand Kind = "rand" // random bytes, base64 in the JSON
)

// Spec is a payload kind and its approximate body size in bytes.
type Spec struct {
	Kind Kind
	Size int
}

func (s Spec) String() string { return string(s.Kind) + "-" + FormatBytes(int64(s.Size)) }

// ParseSpec parses "json-6KB", "rand-1MB", "json-512B".
func ParseSpec(s string) (Spec, error) {
	kind, size, ok := strings.Cut(s, "-")
	if !ok {
		return Spec{}, fmt.Errorf("payload %q: want <json|rand>-<size>", s)
	}
	n, err := ParseBytes(size)
	if err != nil {
		return Spec{}, fmt.Errorf("payload %q: %w", s, err)
	}
	switch Kind(kind) {
	case KindJSON, KindRand:
	default:
		return Spec{}, fmt.Errorf("payload %q: kind must be json or rand", s)
	}
	return Spec{Kind: Kind(kind), Size: int(n)}, nil
}

// ParseBytes parses "512B", "6KB", "1MB", "100MiB" (KB = KiB here).
func ParseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			f, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0, fmt.Errorf("bad size %q", s)
			}
			return int64(f * float64(u.mult)), nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad size %q", s)
	}
	return n, nil
}

// FormatBytes renders n as 6KB / 1MB / 512B.
func FormatBytes(n int64) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%dMB", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%dKB", n>>10)
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

// Generator builds message bodies. Not safe for concurrent use.
type Generator struct {
	rnd *rand.Rand
}

// NewGenerator returns a generator seeded with seed.
func NewGenerator(seed uint64) *Generator {
	return &Generator{rnd: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Body returns a delivery task for harness message id, close to spec.Size
// bytes, with directive for the synthetic handler. Call Stamp just before
// publishing to set the publish time.
func (g *Generator) Body(id string, spec Spec, directive string) []byte {
	meta := models.Metadata{MetaPublished: pubPlaceholder}
	if directive != "" {
		meta[MetaDirective] = directive
	}
	task := models.DeliveryTask{
		Event: models.Event{
			ID:                    id,
			TenantID:              "tenant_" + g.token(10),
			DestinationID:         "des_" + g.token(12),
			MatchedDestinationIDs: []string{},
			Topic:                 g.pick(topics),
			EligibleForRetry:      true,
			Time:                  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(g.rnd.IntN(1e9)) * time.Millisecond),
			Metadata:              meta,
		},
		Attempt: 0,
	}
	task.DestinationID = task.Event.DestinationID
	empty, _ := json.Marshal(task)
	room := spec.Size - len(empty) - 2
	if room < 64 {
		room = 64
	}
	switch spec.Kind {
	case KindRand:
		task.Event.Data = g.randData(room)
	default:
		task.Event.Data = g.jsonData(room)
	}
	b, _ := json.Marshal(task)
	return b
}

// Stamp writes t as the publish time into a body from Body.
func Stamp(body []byte, t time.Time) {
	key := []byte(`"` + MetaPublished + `":"`)
	i := bytes.Index(body, key)
	if i < 0 {
		return
	}
	i += len(key)
	ns := strconv.FormatInt(t.UnixNano(), 10)
	if len(ns) != len(pubPlaceholder) || i+len(ns) > len(body) {
		return
	}
	copy(body[i:], ns)
}

func (g *Generator) randData(room int) models.Data {
	n := (room - 80) * 3 / 4
	if n < 16 {
		n = 16
	}
	raw := make([]byte, n)
	for i := 0; i+8 <= n; i += 8 {
		v := g.rnd.Uint64()
		for j := 0; j < 8; j++ {
			raw[i+j] = byte(v >> (8 * j))
		}
	}
	for i := n - n%8; i < n; i++ {
		raw[i] = byte(g.rnd.Uint32())
	}
	b, _ := json.Marshal(map[string]any{
		"type":         "file.uploaded",
		"id":           "file_" + g.token(16),
		"content_type": "application/octet-stream",
		"content":      base64.StdEncoding.EncodeToString(raw),
	})
	return models.Data(b)
}

func (g *Generator) jsonData(room int) models.Data {
	order := map[string]any{
		"type":       g.pick(topics),
		"id":         "ord_" + g.token(14),
		"created_at": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(g.rnd.IntN(1e9)) * time.Millisecond).Format(time.RFC3339Nano),
		"currency":   g.pick([]string{"USD", "EUR", "GBP", "CAD", "JPY"}),
		"customer": map[string]any{
			"id":    "cus_" + g.token(14),
			"email": g.pick(words) + "." + g.pick(words) + "@" + g.pick(domains),
			"name":  title(g.pick(words)) + " " + title(g.pick(words)),
			"address": map[string]any{
				"line1":   strconv.Itoa(1+g.rnd.IntN(9999)) + " " + title(g.pick(words)) + " " + g.pick([]string{"St", "Ave", "Rd", "Blvd"}),
				"city":    title(g.pick(words)),
				"country": g.pick([]string{"US", "CA", "GB", "DE", "FR", "VN", "JP"}),
				"postal":  g.token(6),
			},
			"tags": []string{g.pick(words), g.pick(words)},
		},
	}
	base, _ := json.Marshal(order)
	var items []map[string]any
	size := len(base)
	for size < room {
		item := g.item()
		ib, _ := json.Marshal(item)
		if size+len(ib)+1 > room && len(items) > 0 {
			break
		}
		items = append(items, item)
		size += len(ib) + 1
	}
	order["line_items"] = items
	b, _ := json.Marshal(order)
	return models.Data(b)
}

func (g *Generator) item() map[string]any {
	nAttr := 2 + g.rnd.IntN(4)
	attrs := make(map[string]any, nAttr)
	for i := 0; i < nAttr; i++ {
		attrs[g.pick(words)] = g.pick(words)
	}
	desc := make([]string, 6+g.rnd.IntN(18))
	for i := range desc {
		desc[i] = g.pick(words)
	}
	return map[string]any{
		"sku":         strings.ToUpper(g.token(8)),
		"name":        title(g.pick(words)) + " " + g.pick(words),
		"quantity":    1 + g.rnd.IntN(12),
		"unit_price":  float64(g.rnd.IntN(100000)) / 100,
		"taxable":     g.rnd.IntN(2) == 0,
		"attributes":  attrs,
		"description": strings.Join(desc, " "),
	}
}

const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func (g *Generator) token(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[g.rnd.IntN(len(alphabet))]
	}
	return string(b)
}

func (g *Generator) pick(from []string) string { return from[g.rnd.IntN(len(from))] }

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var topics = []string{"order.created", "order.updated", "order.fulfilled", "invoice.paid", "customer.updated", "shipment.delivered"}

var domains = []string{"example.com", "example.org", "mail.test", "shop.test"}

var words = strings.Fields(`alpha amber anchor apple arbor atlas aurora autumn badge bamboo banner basil beacon birch bloom blue
bolt breeze bridge bronze cable cactus canyon carbon cedar cell chalk cherry cinder citrus clay cliff cloud clover cobalt comet
copper coral cotton crane crest crystal cypress dawn delta desert dune eagle echo ember emerald falcon fern field flint forest
fossil frost galaxy garnet glacier granite grove harbor hazel heron hollow horizon indigo iris island ivory jade jasmine juniper
kelp lagoon lantern laurel lava lemon lilac linen lotus lunar maple marble meadow mesa mint mist moss nectar nickel nova oak ocean
olive onyx orbit orchid pebble pepper pine plume polar poppy prairie prism quartz rain raven reef ridge river robin ruby saffron
sage sand sapphire shadow shell sierra silver slate snow solar spruce stone storm summit sun tide timber topaz trail tundra
valley velvet violet walnut willow wind winter zephyr zinc order invoice payment refund shipment parcel warehouse ledger account
balance credit debit subscription renewal trial seat license region cluster tenant webhook endpoint retry signature header`)
