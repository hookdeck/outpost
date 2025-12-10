package config

import (
	"strconv"
	"strings"

	"github.com/hookdeck/outpost/internal/portal"
)

type PortalConfig struct {
	ProxyURL               string `yaml:"proxy_url" env:"PORTAL_PROXY_URL" desc:"URL to proxy the Outpost Portal through. If set, Outpost serves the portal assets, and this URL is used as the base. Must be a valid URL." required:"N"`
	RefererURL             string `yaml:"referer_url" env:"PORTAL_REFERER_URL" desc:"The URL were the user is redirected when the JWT token is expired or when the user clicks 'back'. Required if the Outpost Portal is enabled/used." required:"C"`
	FaviconURL             string `yaml:"favicon_url" env:"PORTAL_FAVICON_URL" desc:"URL for the favicon to be used in the Outpost Portal." required:"N"`
	BrandColor             string `yaml:"brand_color" env:"PORTAL_BRAND_COLOR" desc:"Primary brand color (hex code) for theming the Outpost Portal (e.g., '#6122E7'). Also referred to as Accent Color in some contexts." required:"N"`
	Logo                   string `yaml:"logo" env:"PORTAL_LOGO" desc:"URL for the light-mode logo to be displayed in the Outpost Portal." required:"N"`
	LogoDark               string `yaml:"logo_dark" env:"PORTAL_LOGO_DARK" desc:"URL for the dark-mode logo to be displayed in the Outpost Portal." required:"N"`
	OrgName                string `yaml:"org_name" env:"PORTAL_ORGANIZATION_NAME" desc:"Organization name displayed in the Outpost Portal." required:"N"`
	ForceTheme             string `yaml:"force_theme" env:"PORTAL_FORCE_THEME" desc:"Force a specific theme for the Outpost Portal (e.g., 'light', 'dark')." required:"N"`
	DisableOutpostBranding bool   `yaml:"disable_outpost_branding" env:"PORTAL_DISABLE_OUTPOST_BRANDING" desc:"If true, disables Outpost branding in the portal." required:"N"`
}

// GetPortalConfig returns the portal configuration with all necessary fields
func (c *Config) GetPortalConfig() portal.PortalConfig {
	return portal.PortalConfig{
		ProxyURL: c.Portal.ProxyURL,
		Configs: map[string]string{
			"PROXY_URL":                c.Portal.ProxyURL,
			"REFERER_URL":              c.Portal.RefererURL,
			"FAVICON_URL":              c.Portal.FaviconURL,
			"BRAND_COLOR":              c.Portal.BrandColor,
			"LOGO":                     c.Portal.Logo,
			"LOGO_DARK":                c.Portal.LogoDark,
			"ORGANIZATION_NAME":        c.Portal.OrgName,
			"FORCE_THEME":              c.Portal.ForceTheme,
			"TOPICS":                   strings.Join(c.Topics, ","),
			"DISABLE_OUTPOST_BRANDING": strconv.FormatBool(c.Portal.DisableOutpostBranding),
			"DISABLE_TELEMETRY":        strconv.FormatBool(c.DisableTelemetry),
		},
	}
}
