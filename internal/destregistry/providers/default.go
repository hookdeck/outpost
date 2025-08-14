package destregistrydefault

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destazureservicebus"
	"github.com/hookdeck/outpost/internal/destregistry/providers/desthookdeck"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
)

type DestWebhookConfig struct {
	HeaderPrefix                  string
	DisableDefaultEventIDHeader   bool
	DisableDefaultSignatureHeader bool
	DisableDefaultTimestampHeader bool
	DisableDefaultTopicHeader     bool
	SignatureContentTemplate      string
	SignatureHeaderTemplate       string
	SignatureEncoding             string
	SignatureAlgorithm            string
}

type DestAWSKinesisConfig struct {
	MetadataInPayload bool
}

type RegisterDefaultDestinationOptions struct {
	UserAgent  string
	Webhook    *DestWebhookConfig
	AWSKinesis *DestAWSKinesisConfig
}

// RegisterDefault registers the default destination providers with the registry.
// NOTE: The order of registration will determine the order of the provider array
// returned when listing providers.
func RegisterDefault(registry destregistry.Registry, opts RegisterDefaultDestinationOptions) error {
	loader := registry.MetadataLoader()

	webhookOpts := []destwebhook.Option{
		destwebhook.WithUserAgent(opts.UserAgent),
	}
	if opts.Webhook != nil {
		webhookOpts = append(webhookOpts,
			destwebhook.WithHeaderPrefix(opts.Webhook.HeaderPrefix),
			destwebhook.WithDisableDefaultEventIDHeader(opts.Webhook.DisableDefaultEventIDHeader),
			destwebhook.WithDisableDefaultSignatureHeader(opts.Webhook.DisableDefaultSignatureHeader),
			destwebhook.WithDisableDefaultTimestampHeader(opts.Webhook.DisableDefaultTimestampHeader),
			destwebhook.WithDisableDefaultTopicHeader(opts.Webhook.DisableDefaultTopicHeader),
			destwebhook.WithSignatureContentTemplate(opts.Webhook.SignatureContentTemplate),
			destwebhook.WithSignatureHeaderTemplate(opts.Webhook.SignatureHeaderTemplate),
			destwebhook.WithSignatureEncoding(opts.Webhook.SignatureEncoding),
			destwebhook.WithSignatureAlgorithm(opts.Webhook.SignatureAlgorithm),
		)
	}
	webhook, err := destwebhook.New(loader, webhookOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("webhook", webhook)

	hookdeck, err := desthookdeck.New(loader,
		desthookdeck.WithUserAgent(opts.UserAgent))
	if err != nil {
		return err
	}
	registry.RegisterProvider("hookdeck", hookdeck)

	awsSQS, err := destawssqs.New(loader)
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
	awsKinesis, err := destawskinesis.New(loader, awsKinesisOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_kinesis", awsKinesis)

	awsS3, err := destawss3.New(loader)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_s3", awsS3)

	azureServiceBus, err := destazureservicebus.New(loader)
	if err != nil {
		return err
	}
	registry.RegisterProvider("azure_servicebus", azureServiceBus)

	rabbitmq, err := destrabbitmq.New(loader)
	if err != nil {
		return err
	}
	registry.RegisterProvider("rabbitmq", rabbitmq)

	return nil
}
