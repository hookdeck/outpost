package config

// IDGenConfig is the configuration for ID generation
type IDGenConfig struct {
	Type              string `yaml:"type" env:"IDGEN_TYPE" desc:"ID generation type for all entities: uuidv4, uuidv7, nanoid. Default: uuidv4" required:"N"`
	AttemptPrefix     string `yaml:"attempt_prefix" env:"IDGEN_ATTEMPT_PREFIX" desc:"Prefix for attempt IDs, prepended without modification (e.g., 'atm_' produces 'atm_<id>'). Default: empty (no prefix)" required:"N"`
	DestinationPrefix string `yaml:"destination_prefix" env:"IDGEN_DESTINATION_PREFIX" desc:"Prefix for destination IDs, prepended without modification (e.g., 'des_' produces 'des_<id>'). Default: empty (no prefix)" required:"N"`
	EventPrefix       string `yaml:"event_prefix" env:"IDGEN_EVENT_PREFIX" desc:"Prefix for event IDs, prepended without modification (e.g., 'evt_' produces 'evt_<id>'). Default: empty (no prefix)" required:"N"`
}
