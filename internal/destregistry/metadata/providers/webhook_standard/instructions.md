# Webhook Configuration Instructions

To receive events from the webhook destination, you need to set up a webhook endpoint.

A webhook endpoint is a URL that you provide to an HTTP server. When an event is sent to the webhook destination, an HTTP POST request is made to the webhook endpoint with a JSON payload. Information such as the event type will be sent in the HTTP headers.

## Verifying Webhook Signatures

Webhooks include a cryptographic signature for security. To verify:

1. Extract the `webhook-id`, `webhook-timestamp`, and `webhook-signature` headers
2. Construct the signed content: `${webhook-id}.${webhook-timestamp}.${raw_body}`
3. Decode your `whsec_` secret (remove prefix and base64 decode)
4. Compute HMAC-SHA256 signature and compare

Verification libraries are available at: https://github.com/standard-webhooks
