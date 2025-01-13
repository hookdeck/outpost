package config

import (
	"strconv"
	"strings"

	"github.com/hookdeck/outpost/internal/portal"
)

type PortalConfig struct {
	ProxyURL               string `yaml:"proxy_url" env:"PORTAL_PROXY_URL"`
	RefererURL             string `yaml:"referer_url" env:"PORTAL_REFERER_URL"`
	FaviconURL             string `yaml:"favicon_url" env:"PORTAL_FAVICON_URL"`
	Logo                   string `yaml:"logo" env:"PORTAL_LOGO"`
	OrgName                string `yaml:"org_name" env:"PORTAL_ORGANIZATION_NAME"`
	ForceTheme             string `yaml:"force_theme" env:"PORTAL_FORCE_THEME"`
	DisableOutpostBranding bool   `yaml:"disable_outpost_branding" env:"PORTAL_DISABLE_OUTPOST_BRANDING"`
}

// GetPortalConfig returns the portal configuration with all necessary fields
func (c *Config) GetPortalConfig() portal.PortalConfig {
	return portal.PortalConfig{
		ProxyURL: c.Portal.ProxyURL,
		Configs: map[string]string{
			"PROXY_URL":                c.Portal.ProxyURL,
			"REFERER_URL":              c.Portal.RefererURL,
			"FAVICON_URL":              c.Portal.FaviconURL,
			"LOGO":                     c.Portal.Logo,
			"ORGANIZATION_NAME":        c.Portal.OrgName,
			"FORCE_THEME":              c.Portal.ForceTheme,
			"TOPICS":                   strings.Join(c.Topics, ","),
			"DISABLE_OUTPOST_BRANDING": strconv.FormatBool(c.Portal.DisableOutpostBranding),
			"DISABLE_TELEMETRY":        strconv.FormatBool(c.DisableTelemetry),
		},
	}
}
