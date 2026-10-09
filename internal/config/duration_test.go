package config_test

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalText(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr string
	}{
		{in: "90s", want: 90 * time.Second},
		{in: "5m", want: 5 * time.Minute},
		{in: "24h", want: 24 * time.Hour},
		{in: "1h30m", want: 90 * time.Minute},
		{in: "1500ms", want: 1500 * time.Millisecond},
		{in: "3600", want: time.Hour},
		{in: " 60 ", want: time.Minute},
		{in: "+5", want: 5 * time.Second},
		{in: "0", want: 0},
		{in: "-5", want: -5 * time.Second}, // parsed; Validate rejects it
		{in: "-5m", want: -5 * time.Minute},
		{in: "1.5", wantErr: `invalid duration "1.5"`},
		{in: "1d", wantErr: `invalid duration "1d"`},
		{in: "abc", wantErr: `invalid duration "abc"`},
		{in: "", wantErr: `invalid duration ""`},
		{in: "9223372037", wantErr: `duration "9223372037" is out of range`},
		{in: "99999999999999999999", wantErr: `invalid duration "99999999999999999999"`},
		{in: "9223372036", want: 9223372036 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var d config.Duration
			err := d.UnmarshalText([]byte(tt.in))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, d.Duration())
		})
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	type doc struct {
		D config.Duration `yaml:"d"`
	}
	const preset = config.Duration(7 * time.Second)
	tests := []struct {
		name    string
		yaml    string
		want    time.Duration
		wantErr string
	}{
		{name: "duration string", yaml: "d: 90s", want: 90 * time.Second},
		{name: "quoted duration string", yaml: `d: "24h"`, want: 24 * time.Hour},
		{name: "integer seconds", yaml: "d: 3600", want: time.Hour},
		{name: "quoted integer seconds", yaml: `d: "300"`, want: 5 * time.Minute},
		{name: "null keeps the value", yaml: "d:", want: preset.Duration()},
		{name: "absent keeps the value", yaml: "other: 1", want: preset.Duration()},
		{name: "float", yaml: "d: 1.5", wantErr: `line 1: invalid duration "1.5"`},
		{name: "empty string", yaml: `d: ""`, wantErr: `invalid duration ""`},
		{name: "sequence", yaml: "d: [1, 2]", wantErr: "a duration must be a string"},
		{name: "mapping", yaml: "d:\n  h: 1", wantErr: "a duration must be a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := doc{D: preset}
			err := yaml.Unmarshal([]byte(tt.yaml), &v)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, v.D.Duration())
		})
	}
}

func TestDuration_String(t *testing.T) {
	assert.Equal(t, "24h0m0s", config.Duration(24*time.Hour).String())
	assert.Equal(t, "1.5s", config.Duration(1500*time.Millisecond).String())
}

func TestStringList(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		var l config.StringList
		require.NoError(t, l.UnmarshalText([]byte(" localhost, 10.0.0.0/8 ,, ::1 ,")))
		assert.Equal(t, config.StringList{"localhost", "10.0.0.0/8", "::1"}, l)

		require.NoError(t, l.UnmarshalText([]byte(" , ")))
		assert.Empty(t, l)
	})

	type doc struct {
		L config.StringList `yaml:"l"`
	}
	preset := config.StringList{"preset"}
	tests := []struct {
		name    string
		yaml    string
		want    config.StringList
		wantErr string
	}{
		{name: "sequence", yaml: "l: [localhost, ' 10.0.0.0/8 ', '']", want: config.StringList{"localhost", "10.0.0.0/8"}},
		{name: "block sequence", yaml: "l:\n  - 127.0.0.1\n  - ::1\n", want: config.StringList{"127.0.0.1", "::1"}},
		{name: "scalar", yaml: "l: localhost", want: config.StringList{"localhost"}},
		{name: "comma-separated scalar", yaml: `l: "localhost, 10.0.0.0/8"`, want: config.StringList{"localhost", "10.0.0.0/8"}},
		{name: "empty sequence", yaml: "l: []", want: nil},
		{name: "null empties it", yaml: "l:", want: nil},
		{name: "absent keeps the value", yaml: "other: 1", want: preset},
		{name: "mapping", yaml: "l:\n  a: b", wantErr: "a list must be a sequence or a comma-separated string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := doc{L: preset}
			err := yaml.Unmarshal([]byte(tt.yaml), &v)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, v.L)
		})
	}
}
