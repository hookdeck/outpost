package config

// IDGenConfig is the configuration for ID generation
type IDGenConfig struct {
	Type                string `yaml:"type" env:"IDGEN_TYPE" desc:"ID generation type for all entities: uuidv4, uuidv7, nanoid. Default: uuidv4" required:"N"`
	EventPrefix         string `yaml:"event_prefix" env:"IDGEN_EVENT_PREFIX" desc:"Prefix for event IDs, prepended with underscore (e.g., 'evt_123'). Default: empty (no prefix)" required:"N"`
	DestinationPrefix   string `yaml:"destination_prefix" env:"IDGEN_DESTINATION_PREFIX" desc:"Prefix for destination IDs, prepended with underscore (e.g., 'dst_123'). Default: empty (no prefix)" required:"N"`
	DeliveryPrefix      string `yaml:"delivery_prefix" env:"IDGEN_DELIVERY_PREFIX" desc:"Prefix for delivery IDs, prepended with underscore (e.g., 'dlv_123'). Default: empty (no prefix)" required:"N"`
	DeliveryEventPrefix string `yaml:"delivery_event_prefix" env:"IDGEN_DELIVERY_EVENT_PREFIX" desc:"Prefix for delivery event IDs, prepended with underscore (e.g., 'dev_123'). Default: empty (no prefix)" required:"N"`
}
