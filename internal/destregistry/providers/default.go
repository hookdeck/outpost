package destregistrydefault

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destazureservicebus"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destgcppubsub"
	"github.com/hookdeck/outpost/internal/destregistry/providers/desthookdeck"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhookstandard"
)

// WebhookHeaderConfig is the resolved directive for a single webhook system
// header. The config layer collapses the three-state name config (and the
// deprecated DISABLE_* flag) into this: an empty Name with Disabled false means
// "use the default '<prefix>' + key".
type WebhookHeaderConfig struct {
	Name     string
	Disabled bool
}

type DestWebhookConfig struct {
	Mode                     string
	ProxyURL                 string
	HeaderPrefix             string
	EventIDHeader            WebhookHeaderConfig
	SignatureHeader          WebhookHeaderConfig
	TimestampHeader          WebhookHeaderConfig
	TopicHeader              WebhookHeaderConfig
	SignatureContentTemplate string
	SignatureHeaderTemplate  string
	SignatureEncoding        string
	SignatureAlgorithm       string
	SigningSecretTemplate    string
	MaxResponseBodyBytes     int
}

type DestAWSKinesisConfig struct {
	MetadataInPayload bool
}

type RegisterDefaultDestinationOptions struct {
	UserAgent                   string
	IncludeMillisecondTimestamp bool
	Webhook                     *DestWebhookConfig
	AWSKinesis                  *DestAWSKinesisConfig
}

// RegisterDefault registers the default destination providers with the registry.
// NOTE: The order of registration will determine the order of the provider array
// returned when listing providers.
func RegisterDefault(registry destregistry.Registry, opts RegisterDefaultDestinationOptions) error {
	loader := registry.MetadataLoader()

	// Build base publisher options that apply to all providers
	basePublisherOpts := []destregistry.BasePublisherOption{}
	if opts.IncludeMillisecondTimestamp {
		basePublisherOpts = append(basePublisherOpts, destregistry.WithMillisecondTimestamp(opts.IncludeMillisecondTimestamp))
	}

	// Register webhook provider based on mode
	if opts.Webhook != nil && opts.Webhook.Mode == "standard" {
		// Standard Webhooks mode - register webhook_standard as "webhook"
		webhookStandardOpts := []destwebhookstandard.Option{
			destwebhookstandard.WithUserAgent(opts.UserAgent),
			destwebhookstandard.WithProxyURL(opts.Webhook.ProxyURL),
			destwebhookstandard.WithHeaderPrefix(opts.Webhook.HeaderPrefix),
			destwebhookstandard.WithMaxResponseBodyBytes(opts.Webhook.MaxResponseBodyBytes),
		}
		webhookStandard, err := destwebhookstandard.New(loader, basePublisherOpts, webhookStandardOpts...)
		if err != nil {
			return err
		}
		registry.RegisterProvider("webhook", webhookStandard)
	} else {
		// Default mode - register customizable webhook as "webhook"
		webhookOpts := []destwebhook.Option{
			destwebhook.WithUserAgent(opts.UserAgent),
		}
		if opts.Webhook != nil {
			webhookOpts = append(webhookOpts,
				destwebhook.WithProxyURL(opts.Webhook.ProxyURL),
				destwebhook.WithHeaderPrefix(opts.Webhook.HeaderPrefix),
				destwebhook.WithEventIDHeader(opts.Webhook.EventIDHeader.Name, opts.Webhook.EventIDHeader.Disabled),
				destwebhook.WithSignatureHeader(opts.Webhook.SignatureHeader.Name, opts.Webhook.SignatureHeader.Disabled),
				destwebhook.WithTimestampHeader(opts.Webhook.TimestampHeader.Name, opts.Webhook.TimestampHeader.Disabled),
				destwebhook.WithTopicHeader(opts.Webhook.TopicHeader.Name, opts.Webhook.TopicHeader.Disabled),
				destwebhook.WithSignatureContentTemplate(opts.Webhook.SignatureContentTemplate),
				destwebhook.WithSignatureHeaderTemplate(opts.Webhook.SignatureHeaderTemplate),
				destwebhook.WithSignatureEncoding(opts.Webhook.SignatureEncoding),
				destwebhook.WithSignatureAlgorithm(opts.Webhook.SignatureAlgorithm),
				destwebhook.WithSigningSecretTemplate(opts.Webhook.SigningSecretTemplate),
				destwebhook.WithMaxResponseBodyBytes(opts.Webhook.MaxResponseBodyBytes),
			)
		}
		webhook, err := destwebhook.New(loader, basePublisherOpts, webhookOpts...)
		if err != nil {
			return err
		}
		registry.RegisterProvider("webhook", webhook)
	}

	hookdeck, err := desthookdeck.New(loader, basePublisherOpts,
		desthookdeck.WithUserAgent(opts.UserAgent))
	if err != nil {
		return err
	}
	registry.RegisterProvider("hookdeck", hookdeck)

	awsSQS, err := destawssqs.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_sqs", awsSQS)

	awsKinesisOpts := []destawskinesis.Option{}
	if opts.AWSKinesis != nil {
		awsKinesisOpts = append(awsKinesisOpts,
			destawskinesis.WithMetadataInPayload(opts.AWSKinesis.MetadataInPayload),
		)
	}
	awsKinesis, err := destawskinesis.New(loader, basePublisherOpts, awsKinesisOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_kinesis", awsKinesis)

	awsS3, err := destawss3.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_s3", awsS3)

	gcpPubSub, err := destgcppubsub.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("gcp_pubsub", gcpPubSub)

	azureServiceBus, err := destazureservicebus.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("azure_servicebus", azureServiceBus)

	rabbitmq, err := destrabbitmq.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("rabbitmq", rabbitmq)

	kafkaDest, err := destkafka.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("kafka", kafkaDest)

	return nil
}
