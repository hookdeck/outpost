package destregistrydefault

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destaws"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
)

func RegisterDefault(registry destregistry.Registry) error {
	aws, err := destaws.New()
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws", aws)

	rabbitmq, err := destrabbitmq.New()
	if err != nil {
		return err
	}
	registry.RegisterProvider("rabbitmq", rabbitmq)

	webhook, err := destwebhook.New()
	if err != nil {
		return err
	}
	registry.RegisterProvider("webhook", webhook)
	return nil
}
