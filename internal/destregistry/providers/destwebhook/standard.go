package destwebhook

// Standard Webhooks (https://www.standardwebhooks.com) expressed as values of
// the webhook options. Config applies them as the defaults of "standard" mode.
const (
	StandardHeaderPrefix         = "webhook-"
	StandardEventIDHeaderKey     = "id" // "<prefix>id" rather than "<prefix>event-id"
	StandardTimestampFormat      = TimestampFormatUnix
	StandardSignatureContentTmpl = "{{.EventID}}.{{.Timestamp.Unix}}.{{.Body}}"
	StandardSignatureHeaderTmpl  = "v1,{{index .Signatures 0}}{{range slice .Signatures 1}} v1,{{.}}{{end}}"
	StandardEncoding             = "base64"
	StandardSecretEncoding       = SecretEncodingBase64
	StandardSecretPrefix         = "whsec_"
	StandardSigningSecretTmpl    = "whsec_{{.RandomBase64}}"
	StandardMetadataName         = "webhook_standard" // metadata/providers dir with the verification instructions
)
