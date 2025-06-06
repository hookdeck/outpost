// Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.

package components

type WebhookConfig struct {
	// The URL to send the webhook events to.
	URL string `json:"url"`
}

func (o *WebhookConfig) GetURL() string {
	if o == nil {
		return ""
	}
	return o.URL
}
