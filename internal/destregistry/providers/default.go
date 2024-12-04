package destregistrydefault

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destaws"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
)

type DestWebhookConfig struct {
	HeaderPrefix string
}

type RegisterDefaultDestinationOptions struct {
	Webhook *DestWebhookConfig
}

func RegisterDefault(registry destregistry.Registry, opts RegisterDefaultDestinationOptions) error {
	loader := registry.MetadataLoader()

	aws, err := destaws.New(loader)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws", aws)

	rabbitmq, err := destrabbitmq.New(loader)
	if err != nil {
		return err
	}
	registry.RegisterProvider("rabbitmq", rabbitmq)

	webhookOpts := []destwebhook.Option{}
	if opts.Webhook != nil {
		webhookOpts = append(webhookOpts, destwebhook.WithHeaderPrefix(opts.Webhook.HeaderPrefix))
	}
	webhook, err := destwebhook.New(loader, webhookOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("webhook", webhook)
	return nil
}
