// Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.

package components

import (
	"encoding/json"
	"fmt"
)

// DestinationCreateRabbitMQType - Type of the destination. Must be 'rabbitmq'.
type DestinationCreateRabbitMQType string

const (
	DestinationCreateRabbitMQTypeRabbitmq DestinationCreateRabbitMQType = "rabbitmq"
)

func (e DestinationCreateRabbitMQType) ToPointer() *DestinationCreateRabbitMQType {
	return &e
}
func (e *DestinationCreateRabbitMQType) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch v {
	case "rabbitmq":
		*e = DestinationCreateRabbitMQType(v)
		return nil
	default:
		return fmt.Errorf("invalid value for DestinationCreateRabbitMQType: %v", v)
	}
}

type DestinationCreateRabbitMQ struct {
	// Optional user-provided ID. A UUID will be generated if empty.
	ID *string `json:"id,omitempty"`
	// Type of the destination. Must be 'rabbitmq'.
	Type DestinationCreateRabbitMQType `json:"type"`
	// "*" or an array of enabled topics.
	Topics      Topics              `json:"topics"`
	Config      RabbitMQConfig      `json:"config"`
	Credentials RabbitMQCredentials `json:"credentials"`
}

func (o *DestinationCreateRabbitMQ) GetID() *string {
	if o == nil {
		return nil
	}
	return o.ID
}

func (o *DestinationCreateRabbitMQ) GetType() DestinationCreateRabbitMQType {
	if o == nil {
		return DestinationCreateRabbitMQType("")
	}
	return o.Type
}

func (o *DestinationCreateRabbitMQ) GetTopics() Topics {
	if o == nil {
		return Topics{}
	}
	return o.Topics
}

func (o *DestinationCreateRabbitMQ) GetConfig() RabbitMQConfig {
	if o == nil {
		return RabbitMQConfig{}
	}
	return o.Config
}

func (o *DestinationCreateRabbitMQ) GetCredentials() RabbitMQCredentials {
	if o == nil {
		return RabbitMQCredentials{}
	}
	return o.Credentials
}
