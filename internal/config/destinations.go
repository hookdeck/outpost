package config

import (
	"fmt"

	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/version"
)

// DestinationsConfig is the main configuration for all destination types
type DestinationsConfig struct {
	MetadataPath                string                      `yaml:"metadata_path" env:"DESTINATIONS_METADATA_PATH" desc:"Path to the directory containing custom destination type definitions." required:"N"`
	IncludeMillisecondTimestamp bool                        `yaml:"include_millisecond_timestamp" env:"DESTINATIONS_INCLUDE_MILLISECOND_TIMESTAMP" desc:"If true, includes a 'timestamp-ms' field with millisecond precision in destination metadata. Useful for load testing and debugging." required:"N"`
	Webhook                     DestinationWebhookConfig    `yaml:"webhook" desc:"Configuration specific to webhook destinations."`
	AWSKinesis                  DestinationAWSKinesisConfig `yaml:"aws_kinesis" desc:"Configuration specific to AWS Kinesis destinations."`
}

func (c *DestinationsConfig) ToConfig(cfg *Config) destregistrydefault.RegisterDefaultDestinationOptions {
	userAgent := cfg.HTTPUserAgent
	if userAgent == "" {
		userAgent = fmt.Sprintf("Outpost/%s", version.Version())
	}

	return destregistrydefault.RegisterDefaultDestinationOptions{
		UserAgent:                   userAgent,
		IncludeMillisecondTimestamp: c.IncludeMillisecondTimestamp,
		Webhook:                     c.Webhook.toConfig(),
		AWSKinesis:                  c.AWSKinesis.toConfig(),
	}
}

// Webhook configuration
type DestinationWebhookConfig struct {
	// ProxyURL may contain authentication credentials (e.g., http://user:pass@proxy:8080)
	// and should be treated as sensitive.
	// TODO: Implement sensitive value handling - https://github.com/hookdeck/outpost/issues/480
	Mode                          string `yaml:"mode" env:"DESTINATIONS_WEBHOOK_MODE" desc:"Webhook mode: 'default' for customizable webhooks or 'standard' for Standard Webhooks specification compliance. Defaults to 'default'." required:"N"`
	ProxyURL                      string `yaml:"proxy_url" env:"DESTINATIONS_WEBHOOK_PROXY_URL" desc:"Proxy URL for routing webhook requests through a proxy server. Supports HTTP and HTTPS proxies. When configured, all outgoing webhook traffic will be routed through the specified proxy." required:"N"`
	HeaderPrefix                  string `yaml:"header_prefix" env:"DESTINATIONS_WEBHOOK_HEADER_PREFIX" desc:"Prefix for metadata headers added to webhook requests. Defaults to 'x-outpost-' in 'default' mode and 'webhook-' in 'standard' mode. Set to whitespace (e.g. ' ') to disable the prefix entirely." required:"N"`
	DisableDefaultEventIDHeader   bool   `yaml:"disable_default_event_id_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER" desc:"If true, disables adding the default 'X-Outpost-Event-Id' header to webhook requests. Only applies to 'default' mode." required:"N"`
	DisableDefaultSignatureHeader bool   `yaml:"disable_default_signature_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_SIGNATURE_HEADER" desc:"If true, disables adding the default 'X-Outpost-Signature' header to webhook requests. Only applies to 'default' mode." required:"N"`
	DisableDefaultTimestampHeader bool   `yaml:"disable_default_timestamp_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TIMESTAMP_HEADER" desc:"If true, disables adding the default 'X-Outpost-Timestamp' header to webhook requests. Only applies to 'default' mode." required:"N"`
	DisableDefaultTopicHeader     bool   `yaml:"disable_default_topic_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TOPIC_HEADER" desc:"If true, disables adding the default 'X-Outpost-Topic' header to webhook requests. Only applies to 'default' mode." required:"N"`
	SignatureContentTemplate      string `yaml:"signature_content_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_CONTENT_TEMPLATE" desc:"Go template for constructing the content to be signed for webhook requests. Only applies to 'default' mode." required:"N"`
	SignatureHeaderTemplate       string `yaml:"signature_header_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_TEMPLATE" desc:"Go template for the value of the signature header. Only applies to 'default' mode." required:"N"`
	SignatureEncoding             string `yaml:"signature_encoding" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ENCODING" desc:"Encoding for the signature (e.g., 'hex', 'base64'). Only applies to 'default' mode." required:"N"`
	SignatureAlgorithm            string `yaml:"signature_algorithm" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ALGORITHM" desc:"Algorithm used for signing webhook requests (e.g., 'hmac-sha256'). Only applies to 'default' mode." required:"N"`
	SigningSecretTemplate         string `yaml:"signing_secret_template" env:"DESTINATIONS_WEBHOOK_SIGNING_SECRET_TEMPLATE" desc:"Go template for generating webhook signing secrets. Available variables: {{.RandomHex}} (64-char hex), {{.RandomBase64}} (base64-encoded), {{.RandomAlphanumeric}} (32-char alphanumeric). Defaults to 'whsec_{{.RandomHex}}'. Only applies to 'default' mode." required:"N"`
}

// toConfig converts WebhookConfig to the provider config - private since it's only used internally
// Config guarantees all required values are set via setDefaults()
func (c *DestinationWebhookConfig) toConfig() *destregistrydefault.DestWebhookConfig {
	// HeaderPrefix: config provides mode-specific defaults
	// - user sets "" → config applies default based on mode ("x-outpost-" or "webhook-")
	// - user sets " " → whitespace passes through → provider trims to "" (disabled)
	// - user sets explicit value → passes through as-is
	headerPrefix := c.HeaderPrefix
	if headerPrefix == "" {
		// Apply mode-specific default only when truly empty (not whitespace)
		if c.Mode == "standard" {
			headerPrefix = "webhook-"
		} else {
			headerPrefix = "x-outpost-"
		}
	}

	return &destregistrydefault.DestWebhookConfig{
		Mode:                          c.Mode,
		ProxyURL:                      c.ProxyURL,
		HeaderPrefix:                  headerPrefix,
		DisableDefaultEventIDHeader:   c.DisableDefaultEventIDHeader,
		DisableDefaultSignatureHeader: c.DisableDefaultSignatureHeader,
		DisableDefaultTimestampHeader: c.DisableDefaultTimestampHeader,
		DisableDefaultTopicHeader:     c.DisableDefaultTopicHeader,
		SignatureContentTemplate:      c.SignatureContentTemplate,
		SignatureHeaderTemplate:       c.SignatureHeaderTemplate,
		SignatureEncoding:             c.SignatureEncoding,
		SignatureAlgorithm:            c.SignatureAlgorithm,
		SigningSecretTemplate:         c.SigningSecretTemplate,
	}
}

// AWS Kinesis configuration
type DestinationAWSKinesisConfig struct {
	MetadataInPayload bool `yaml:"metadata_in_payload" env:"DESTINATIONS_AWS_KINESIS_METADATA_IN_PAYLOAD" desc:"If true, includes Outpost metadata (event ID, topic, etc.) within the Kinesis record payload." required:"N"`
}

// toConfig converts AWSKinesisConfig to the provider config
func (c *DestinationAWSKinesisConfig) toConfig() *destregistrydefault.DestAWSKinesisConfig {
	return &destregistrydefault.DestAWSKinesisConfig{
		MetadataInPayload: c.MetadataInPayload,
	}
}
