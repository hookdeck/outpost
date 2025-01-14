package config

import (
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
)

type DestinationsConfig struct {
	MetadataPath string                   `yaml:"metadata_path" env:"DESTINATIONS_METADATA_PATH"`
	Webhook      DestinationWebhookConfig `yaml:"webhook"`
}

func (c *DestinationsConfig) ToConfig() destregistrydefault.RegisterDefaultDestinationOptions {
	return destregistrydefault.RegisterDefaultDestinationOptions{
		Webhook: c.Webhook.toConfig(),
	}
}

type DestinationWebhookConfig struct {
	HeaderPrefix                  string `yaml:"header_prefix" env:"DESTINATIONS_WEBHOOK_HEADER_PREFIX"`
	DisableDefaultEventIDHeader   bool   `yaml:"disable_default_event_id_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER"`
	DisableDefaultSignatureHeader bool   `yaml:"disable_default_signature_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_SIGNATURE_HEADER"`
	DisableDefaultTimestampHeader bool   `yaml:"disable_default_timestamp_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TIMESTAMP_HEADER"`
	DisableDefaultTopicHeader     bool   `yaml:"disable_default_topic_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TOPIC_HEADER"`
	SignatureContentTemplate      string `yaml:"signature_content_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_CONTENT_TEMPLATE"`
	SignatureHeaderTemplate       string `yaml:"signature_header_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_TEMPLATE"`
	SignatureEncoding             string `yaml:"signature_encoding" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ENCODING"`
	SignatureAlgorithm            string `yaml:"signature_algorithm" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ALGORITHM"`
}

// toConfig is now private since it's only used internally by DestinationsConfig.ToConfig
func (c *DestinationWebhookConfig) toConfig() *destregistrydefault.DestWebhookConfig {
	return &destregistrydefault.DestWebhookConfig{
		HeaderPrefix:                  c.HeaderPrefix,
		DisableDefaultEventIDHeader:   c.DisableDefaultEventIDHeader,
		DisableDefaultSignatureHeader: c.DisableDefaultSignatureHeader,
		DisableDefaultTimestampHeader: c.DisableDefaultTimestampHeader,
		DisableDefaultTopicHeader:     c.DisableDefaultTopicHeader,
		SignatureContentTemplate:      c.SignatureContentTemplate,
		SignatureHeaderTemplate:       c.SignatureHeaderTemplate,
		SignatureEncoding:             c.SignatureEncoding,
		SignatureAlgorithm:            c.SignatureAlgorithm,
	}
}
