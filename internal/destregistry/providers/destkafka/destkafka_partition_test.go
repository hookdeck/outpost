package destkafka

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Expected partition per key and count, from the Sarama-compatible FNV-1a
// placement (fnv32a modulo, negated when negative). Changing it re-routes
// keys of existing topics.
var partitionCounts = []int{1, 2, 3, 7, 12, 32, 100}

var partitionTable = []struct {
	key  string
	want []int
}{
	{key: "", want: []int{0, 1, 0, 2, 3, 27, 35}},
	{key: "a", want: []int{0, 0, 0, 6, 0, 20, 76}},
	{key: "evt_123", want: []int{0, 1, 2, 2, 11, 7, 35}},
	{key: "order-42", want: []int{0, 0, 2, 4, 8, 20, 52}},
	{key: "tenant_1", want: []int{0, 1, 1, 2, 1, 13, 25}},
	{key: "user@example.com", want: []int{0, 1, 2, 5, 5, 5, 9}},
	{key: "123", want: []int{0, 1, 2, 5, 11, 27, 11}},
	{key: "true", want: []int{0, 1, 0, 2, 9, 5, 21}},
	{key: "1.5", want: []int{0, 1, 1, 2, 7, 7, 63}},
	{key: "ünïcödé", want: []int{0, 0, 1, 6, 10, 18, 34}},
	{key: "a-much-longer-partition-key-value-0123456789", want: []int{0, 1, 1, 5, 1, 9, 25}},
	{key: "evt_00000000", want: []int{0, 1, 0, 1, 9, 25, 1}},
	{key: "evt_00007919", want: []int{0, 1, 1, 1, 1, 5, 69}},
	{key: "evt_00015838", want: []int{0, 0, 1, 0, 10, 22, 70}},
	{key: "evt_00023757", want: []int{0, 1, 0, 5, 3, 19, 91}},
	{key: "evt_00031676", want: []int{0, 0, 2, 4, 8, 16, 88}},
	{key: "evt_00039595", want: []int{0, 0, 2, 3, 2, 6, 26}},
	{key: "evt_00047514", want: []int{0, 0, 1, 0, 10, 10, 38}},
	{key: "evt_00055433", want: []int{0, 1, 0, 4, 9, 9, 17}},
}

func TestPartitioner_KeyPlacement(t *testing.T) {
	t.Parallel()

	p := newPartitioner().ForTopic("t")
	for _, tc := range partitionTable {
		for i, n := range partitionCounts {
			rec := &kgo.Record{Key: []byte(tc.key)}
			assert.Equal(t, tc.want[i], p.Partition(rec, n), "key %q over %d partitions", tc.key, n)
		}
	}
}
