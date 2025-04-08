package destregistrydefault

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
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
	Webhook    *DestWebhookConfig
	AWSKinesis *DestAWSKinesisConfig
}

func RegisterDefault(registry destregistry.Registry, opts RegisterDefaultDestinationOptions) error {
	loader := registry.MetadataLoader()

	awsSQS, err := destawssqs.New(loader)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_sqs", awsSQS)

	rabbitmq, err := destrabbitmq.New(loader)
	if err != nil {
		return err
	}
	registry.RegisterProvider("rabbitmq", rabbitmq)

	// Register AWS Kinesis destination
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

	webhookOpts := []destwebhook.Option{}
	if opts.Webhook != nil {
		webhookOpts = []destwebhook.Option{
			destwebhook.WithHeaderPrefix(opts.Webhook.HeaderPrefix),
			destwebhook.WithDisableDefaultEventIDHeader(opts.Webhook.DisableDefaultEventIDHeader),
			destwebhook.WithDisableDefaultSignatureHeader(opts.Webhook.DisableDefaultSignatureHeader),
			destwebhook.WithDisableDefaultTimestampHeader(opts.Webhook.DisableDefaultTimestampHeader),
			destwebhook.WithDisableDefaultTopicHeader(opts.Webhook.DisableDefaultTopicHeader),
			destwebhook.WithSignatureContentTemplate(opts.Webhook.SignatureContentTemplate),
			destwebhook.WithSignatureHeaderTemplate(opts.Webhook.SignatureHeaderTemplate),
			destwebhook.WithSignatureEncoding(opts.Webhook.SignatureEncoding),
			destwebhook.WithSignatureAlgorithm(opts.Webhook.SignatureAlgorithm),
		}
	}
	webhook, err := destwebhook.New(loader, webhookOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("webhook", webhook)
	return nil
}
