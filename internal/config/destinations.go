package config

import (
	"fmt"

	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/version"
)

// DestinationsConfig is the main configuration for all destination types
type DestinationsConfig struct {
	MetadataPath                string                      `yaml:"metadata_path" env:"DESTINATIONS_METADATA_PATH" desc:"Path to the directory containing custom destination type definitions. This can be overridden by the root-level 'destination_metadata_path' if also set." required:"N"`
	IncludeMillisecondTimestamp bool                        `yaml:"include_millisecond_timestamp" env:"DESTINATIONS_INCLUDE_MILLISECOND_TIMESTAMP" desc:"If true, includes a 'timestamp-ms' field with millisecond precision in destination metadata. Useful for load testing and debugging." required:"N"`
	Webhook                     DestinationWebhookConfig    `yaml:"webhook" desc:"Configuration specific to webhook destinations."`
	AWSKinesis                  DestinationAWSKinesisConfig `yaml:"aws_kinesis" desc:"Configuration specific to AWS Kinesis destinations."`
}

func (c *DestinationsConfig) ToConfig(cfg *Config) destregistrydefault.RegisterDefaultDestinationOptions {
	userAgent := cfg.HTTPUserAgent
	if userAgent == "" {
		if cfg.OrganizationName == "" {
			userAgent = fmt.Sprintf("Outpost/%s", version.Version())
		} else {
			userAgent = fmt.Sprintf("%s/%s", cfg.OrganizationName, version.Version())
		}
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
	ProxyURL                      string `yaml:"proxy_url" env:"DESTINATIONS_WEBHOOK_PROXY_URL" desc:"Proxy URL for routing webhook requests through a proxy server. Supports HTTP and HTTPS proxies. When configured, all outgoing webhook traffic will be routed through the specified proxy." required:"N"`
	HeaderPrefix                  string `yaml:"header_prefix" env:"DESTINATIONS_WEBHOOK_HEADER_PREFIX" desc:"Prefix for custom headers added to webhook requests (e.g., 'X-MyOrg-')." required:"N"`
	DisableDefaultEventIDHeader   bool   `yaml:"disable_default_event_id_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER" desc:"If true, disables adding the default 'X-Outpost-Event-Id' header to webhook requests." required:"N"`
	DisableDefaultSignatureHeader bool   `yaml:"disable_default_signature_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_SIGNATURE_HEADER" desc:"If true, disables adding the default 'X-Outpost-Signature' header to webhook requests." required:"N"`
	DisableDefaultTimestampHeader bool   `yaml:"disable_default_timestamp_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TIMESTAMP_HEADER" desc:"If true, disables adding the default 'X-Outpost-Timestamp' header to webhook requests." required:"N"`
	DisableDefaultTopicHeader     bool   `yaml:"disable_default_topic_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TOPIC_HEADER" desc:"If true, disables adding the default 'X-Outpost-Topic' header to webhook requests." required:"N"`
	SignatureContentTemplate      string `yaml:"signature_content_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_CONTENT_TEMPLATE" desc:"Go template for constructing the content to be signed for webhook requests." required:"N"`
	SignatureHeaderTemplate       string `yaml:"signature_header_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_TEMPLATE" desc:"Go template for the value of the signature header." required:"N"`
	SignatureEncoding             string `yaml:"signature_encoding" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ENCODING" desc:"Encoding for the signature (e.g., 'hex', 'base64')." required:"N"`
	SignatureAlgorithm            string `yaml:"signature_algorithm" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ALGORITHM" desc:"Algorithm used for signing webhook requests (e.g., 'hmac-sha256')." required:"N"`
}

// toConfig converts WebhookConfig to the provider config - private since it's only used internally
func (c *DestinationWebhookConfig) toConfig() *destregistrydefault.DestWebhookConfig {
	return &destregistrydefault.DestWebhookConfig{
		ProxyURL:                      c.ProxyURL,
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
