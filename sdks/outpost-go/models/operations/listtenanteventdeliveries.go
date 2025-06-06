// Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.

package operations

import (
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

type ListTenantEventDeliveriesGlobals struct {
	TenantID *string `pathParam:"style=simple,explode=false,name=tenant_id"`
}

func (o *ListTenantEventDeliveriesGlobals) GetTenantID() *string {
	if o == nil {
		return nil
	}
	return o.TenantID
}

type ListTenantEventDeliveriesRequest struct {
	// The ID of the tenant. Required when using AdminApiKey authentication.
	TenantID *string `pathParam:"style=simple,explode=false,name=tenant_id"`
	// The ID of the event.
	EventID string `pathParam:"style=simple,explode=false,name=event_id"`
}

func (o *ListTenantEventDeliveriesRequest) GetTenantID() *string {
	if o == nil {
		return nil
	}
	return o.TenantID
}

func (o *ListTenantEventDeliveriesRequest) GetEventID() string {
	if o == nil {
		return ""
	}
	return o.EventID
}

type ListTenantEventDeliveriesResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
	// A list of delivery attempts.
	DeliveryAttempts []components.DeliveryAttempt
}

func (o *ListTenantEventDeliveriesResponse) GetHTTPMeta() components.HTTPMetadata {
	if o == nil {
		return components.HTTPMetadata{}
	}
	return o.HTTPMeta
}

func (o *ListTenantEventDeliveriesResponse) GetDeliveryAttempts() []components.DeliveryAttempt {
	if o == nil {
		return nil
	}
	return o.DeliveryAttempts
}
