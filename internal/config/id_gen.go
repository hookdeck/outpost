package config

// IDGenConfig is the configuration for ID generation
type IDGenConfig struct {
	Type              string `yaml:"type" env:"ID_GEN_TYPE" desc:"ID generation type for all entities: uuidv4, uuidv7, nanoid. Default: uuidv4" required:"N"`
	EventPrefix       string `yaml:"event_prefix" env:"ID_GEN_EVENT_PREFIX" desc:"Prefix for event IDs, prepended with underscore (e.g., 'evt_123'). Default: empty (no prefix)" required:"N"`
	DestinationPrefix string `yaml:"destination_prefix" env:"ID_GEN_DESTINATION_PREFIX" desc:"Prefix for destination IDs, prepended with underscore (e.g., 'dst_123'). Default: empty (no prefix)" required:"N"`
}
