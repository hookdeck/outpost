package idgen

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestConfigure(t *testing.T) {
	tests := []struct {
		name        string
		idType      string
		wantErr     bool
		description string
	}{
		{
			name:        "empty type uses default",
			idType:      "",
			wantErr:     false,
			description: "should use default uuidv4",
		},
		{
			name:        "valid uuidv4 type",
			idType:      "uuidv4",
			wantErr:     false,
			description: "should accept uuidv4 type",
		},
		{
			name:        "valid uuidv7 type",
			idType:      "uuidv7",
			wantErr:     false,
			description: "should accept uuidv7 type",
		},
		{
			name:        "valid nanoid type",
			idType:      "nanoid",
			wantErr:     false,
			description: "should accept nanoid type",
		},
		{
			name:        "invalid type",
			idType:      "invalid",
			wantErr:     true,
			description: "should fail on invalid type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Configure(IDGenConfig{
				Type:        tt.idType,
				EventPrefix: "",
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("Configure() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvent_Generate(t *testing.T) {
	tests := []struct {
		name     string
		idType   string
		prefix   string
		validate func(t *testing.T, id string)
	}{
		{
			name:   "uuidv4 generates valid UUID",
			idType: "uuidv4",
			prefix: "",
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
			name:   "uuidv7 generates valid UUID",
			idType: "uuidv7",
			prefix: "",
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
			name:   "nanoid generates valid ID",
			idType: "nanoid",
			prefix: "",
			validate: func(t *testing.T, id string) {
				if len(id) != 26 {
					t.Errorf("Nanoid should be 26 characters, got %d: %s", len(id), id)
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
			name:   "uuidv4 with prefix",
			idType: "uuidv4",
			prefix: "evt",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Configure(IDGenConfig{
				Type:        tt.idType,
				EventPrefix: tt.prefix,
			})
			if err != nil {
				t.Fatalf("Configure() error = %v", err)
			}

			id := Event()
			if id == "" {
				t.Error("Event() returned empty string")
			}

			tt.validate(t, id)
		})
	}
}

func TestEvent_Uniqueness(t *testing.T) {
	err := Configure(IDGenConfig{
		Type:        "uuidv4",
		EventPrefix: "",
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	// Generate multiple IDs and ensure they're unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := Event()
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

	t.Run("uses configured type and prefix", func(t *testing.T) {
		err := Configure(IDGenConfig{
			Type:        "uuidv4",
			EventPrefix: "evt",
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

func BenchmarkEvent_UUIDv4(b *testing.B) {
	Configure(IDGenConfig{Type: "uuidv4", EventPrefix: ""})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Event()
	}
}

func BenchmarkEvent_UUIDv7(b *testing.B) {
	Configure(IDGenConfig{Type: "uuidv7", EventPrefix: ""})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Event()
	}
}

func BenchmarkEvent_Nanoid(b *testing.B) {
	Configure(IDGenConfig{Type: "nanoid", EventPrefix: ""})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Event()
	}
}
