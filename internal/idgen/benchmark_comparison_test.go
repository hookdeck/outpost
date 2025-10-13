package idgen

import (
	"testing"

	"github.com/google/uuid"
)

// Benchmark direct UUID generation (original approach)
func BenchmarkDirectUUIDv4(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = uuid.New().String()
	}
}

func BenchmarkDirectUUIDv7(b *testing.B) {
	for i := 0; i < b.N; i++ {
		id, _ := uuid.NewV7()
		_ = id.String()
	}
}

// Benchmark template-based generation (new approach)
func BenchmarkTemplateUUIDv4(b *testing.B) {
	gen, _ := NewIDGenerator("{{uuidv4}}")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate()
	}
}

func BenchmarkTemplateUUIDv7(b *testing.B) {
	gen, _ := NewIDGenerator("{{uuidv7}}")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate()
	}
}
