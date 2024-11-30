package destregistrydefault

import (
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destaws"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
)

func RegisterDefault(registry destregistry.Registry) {
	registry.RegisterProvider("aws", destaws.New())
	registry.RegisterProvider("rabbitmq", destrabbitmq.New())
	registry.RegisterProvider("webhook", destwebhook.New())
}
