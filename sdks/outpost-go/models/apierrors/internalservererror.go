// Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.

package apierrors

import (
	"encoding/json"
)

// InternalServerError - A collection of status codes that generally mean the server failed in an unexpected way
type InternalServerError struct {
	Message              *string        `json:"message,omitempty"`
	AdditionalProperties map[string]any `additionalProperties:"true" json:"-"`
}

var _ error = &InternalServerError{}

func (e *InternalServerError) Error() string {
	data, _ := json.Marshal(e)
	return string(data)
}
