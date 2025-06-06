// Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.

package components

type PublishResponse struct {
	// The ID of the event that was accepted for publishing. This will be the ID provided in the request's `id` field if present, otherwise it's a server-generated UUID.
	ID string `json:"id"`
}

func (o *PublishResponse) GetID() string {
	if o == nil {
		return ""
	}
	return o.ID
}
