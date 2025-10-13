package config

// IDTemplateConfig is the configuration for ID generation templates
type IDTemplateConfig struct {
	Event string `yaml:"event" env:"ID_TEMPLATE_EVENT" desc:"Go template for generating event IDs. Available functions: uuidv4, uuidv7, nanoid. Default: '{{uuidv4}}'" required:"N"`
}
