package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestFormatFieldValue(t *testing.T) {
	tests := []struct {
		name  string
		field zap.Field
		want  string
	}{
		{"bool true", zap.Bool("k", true), "true"},
		{"bool false", zap.Bool("k", false), "false"},
		{"string", zap.String("k", "value"), "value"},
		{"int", zap.Int("k", 42), "42"},
		{"strings array", zap.Strings("k", []string{"a", "b"}), "[a b]"},
		{"ints array", zap.Ints("k", []int{1, 2}), "[1 2]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatFieldValue(tt.field))
		})
	}
}

func TestPrintConfigList(t *testing.T) {
	var buf bytes.Buffer
	printConfigList(&buf, []zap.Field{
		zap.String("service", "api"),
		zap.Bool("api_key_configured", true),
		zap.Int("api_port", 3333),
	})

	assert.Equal(t,
		"api_key_configured  true\n"+
			"api_port            3333\n"+
			"service             api\n",
		buf.String(),
	)
}
