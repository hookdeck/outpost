package idgen

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewIDGenerator(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		wantErr     bool
		description string
	}{
		{
			name:        "empty template uses default",
			template:    "",
			wantErr:     false,
			description: "should use default uuidv4",
		},
		{
			name:        "valid uuidv4 template",
			template:    "{{uuidv4}}",
			wantErr:     false,
			description: "should accept uuidv4 function",
		},
		{
			name:        "valid uuidv7 template",
			template:    "{{uuidv7}}",
			wantErr:     false,
			description: "should accept uuidv7 function",
		},
		{
			name:        "valid nanoid template",
			template:    "{{nanoid}}",
			wantErr:     false,
			description: "should accept nanoid function",
		},
		{
			name:        "composite template",
			template:    "evt_{{uuidv4}}",
			wantErr:     false,
			description: "should accept composite templates",
		},
		{
			name:        "invalid template syntax",
			template:    "{{invalid",
			wantErr:     true,
			description: "should fail on invalid template syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewIDGenerator(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIDGenerator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && gen == nil {
				t.Error("NewIDGenerator() returned nil generator without error")
			}
		})
	}
}

func TestIDGenerator_Generate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		validate func(t *testing.T, id string)
	}{
		{
			name:     "uuidv4 generates valid UUID",
			template: "{{uuidv4}}",
			validate: func(t *testing.T, id string) {
				if _, err := uuid.Parse(id); err != nil {
					t.Errorf("Generated ID is not a valid UUID: %s", id)
				}
				// UUIDv4 has version 4 in the correct position
				if !strings.Contains(id, "-4") {
					t.Errorf("Generated ID is not a UUID v4: %s", id)
				}
			},
		},
		{
			name:     "uuidv7 generates valid UUID",
			template: "{{uuidv7}}",
			validate: func(t *testing.T, id string) {
				if _, err := uuid.Parse(id); err != nil {
					t.Errorf("Generated ID is not a valid UUID: %s", id)
				}
				// UUIDv7 has version 7 in the correct position
				parsed, _ := uuid.Parse(id)
				if parsed.Version() != 7 {
					t.Errorf("Generated ID is not a UUID v7: %s (version: %d)", id, parsed.Version())
				}
			},
		},
		{
			name:     "nanoid generates valid ID",
			template: "{{nanoid}}",
			validate: func(t *testing.T, id string) {
				if len(id) != 21 {
					t.Errorf("Nanoid should be 21 characters, got %d: %s", len(id), id)
				}
				// Check it only contains valid characters
				const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
				for _, c := range id {
					if !strings.ContainsRune(alphabet, c) {
						t.Errorf("Nanoid contains invalid character: %c", c)
					}
				}
			},
		},
		{
			name:     "composite template with prefix",
			template: "evt_{{uuidv4}}",
			validate: func(t *testing.T, id string) {
				if !strings.HasPrefix(id, "evt_") {
					t.Errorf("ID should have prefix 'evt_', got: %s", id)
				}
				uuidPart := strings.TrimPrefix(id, "evt_")
				if _, err := uuid.Parse(uuidPart); err != nil {
					t.Errorf("UUID part is not valid: %s", uuidPart)
				}
			},
		},
		{
			name:     "multiple function calls",
			template: "{{uuidv4}}_{{nanoid}}",
			validate: func(t *testing.T, id string) {
				parts := strings.Split(id, "_")
				if len(parts) != 2 {
					t.Errorf("Expected 2 parts separated by underscore, got %d", len(parts))
					return
				}
				// First part should be UUID
				if _, err := uuid.Parse(parts[0]); err != nil {
					t.Errorf("First part is not a valid UUID: %s", parts[0])
				}
				// Second part should be nanoid (21 chars)
				if len(parts[1]) != 21 {
					t.Errorf("Second part should be 21 characters, got %d", len(parts[1]))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewIDGenerator(tt.template)
			if err != nil {
				t.Fatalf("NewIDGenerator() error = %v", err)
			}

			id, err := gen.Generate()
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			if id == "" {
				t.Error("Generate() returned empty string")
			}

			tt.validate(t, id)
		})
	}
}

func TestIDGenerator_GenerateUniqueness(t *testing.T) {
	gen, err := NewIDGenerator("{{uuidv4}}")
	if err != nil {
		t.Fatalf("NewIDGenerator() error = %v", err)
	}

	// Generate multiple IDs and ensure they're unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := gen.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if ids[id] {
			t.Errorf("Generated duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

func TestEvent(t *testing.T) {
	t.Run("generates UUID v4 by default", func(t *testing.T) {
		id := Event()
		if id == "" {
			t.Error("Event() returned empty string")
		}
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("Event() returned invalid UUID: %s", id)
		}
	})

	t.Run("uses configured template", func(t *testing.T) {
		err := Configure(IDTemplateConfig{
			Event: "evt_{{uuidv4}}",
		})
		if err != nil {
			t.Fatalf("Configure() error = %v", err)
		}

		id := Event()
		if !strings.HasPrefix(id, "evt_") {
			t.Errorf("Event() = %v, want prefix 'evt_'", id)
		}
	})
}

func BenchmarkIDGenerator_UUIDv4(b *testing.B) {
	gen, _ := NewIDGenerator("{{uuidv4}}")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen.Generate()
	}
}

func BenchmarkIDGenerator_UUIDv7(b *testing.B) {
	gen, _ := NewIDGenerator("{{uuidv7}}")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen.Generate()
	}
}

func BenchmarkIDGenerator_Nanoid(b *testing.B) {
	gen, _ := NewIDGenerator("{{nanoid}}")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen.Generate()
	}
}
