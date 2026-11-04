package mcpclient

const (
	CodeMethodNotFound        = -32601
	CodeInvalidParams         = -32602
	CodeHeaderMismatch        = -32020
	CodeNotFound              = -32023
	CodeForbidden             = -32024
	CodeResourceExhausted     = -32025
	CodeUnsupported           = -32026
	CodeCallbackEndpointError = -32027
)

// LastErrorCategories are the values of deliveryStatus.lastError.
var LastErrorCategories = []string{"connection_refused", "timeout", "tls_error", "http_4xx", "http_5xx", "challenge_failed"}
