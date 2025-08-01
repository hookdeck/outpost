// Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.

package components

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hookdeck/outpost/sdks/outpost-go/internal/utils"
)

type DestinationCreateType string

const (
	DestinationCreateTypeWebhook         DestinationCreateType = "webhook"
	DestinationCreateTypeAwsSqs          DestinationCreateType = "aws_sqs"
	DestinationCreateTypeRabbitmq        DestinationCreateType = "rabbitmq"
	DestinationCreateTypeHookdeck        DestinationCreateType = "hookdeck"
	DestinationCreateTypeAwsKinesis      DestinationCreateType = "aws_kinesis"
	DestinationCreateTypeAzureServicebus DestinationCreateType = "azure_servicebus"
)

type DestinationCreate struct {
	DestinationCreateWebhook         *DestinationCreateWebhook         `queryParam:"inline"`
	DestinationCreateAWSSQS          *DestinationCreateAWSSQS          `queryParam:"inline"`
	DestinationCreateRabbitMQ        *DestinationCreateRabbitMQ        `queryParam:"inline"`
	DestinationCreateHookdeck        *DestinationCreateHookdeck        `queryParam:"inline"`
	DestinationCreateAWSKinesis      *DestinationCreateAWSKinesis      `queryParam:"inline"`
	DestinationCreateAzureServiceBus *DestinationCreateAzureServiceBus `queryParam:"inline"`

	Type DestinationCreateType
}

func CreateDestinationCreateWebhook(webhook DestinationCreateWebhook) DestinationCreate {
	typ := DestinationCreateTypeWebhook

	typStr := DestinationCreateWebhookType(typ)
	webhook.Type = typStr

	return DestinationCreate{
		DestinationCreateWebhook: &webhook,
		Type:                     typ,
	}
}

func CreateDestinationCreateAwsSqs(awsSqs DestinationCreateAWSSQS) DestinationCreate {
	typ := DestinationCreateTypeAwsSqs

	typStr := DestinationCreateAWSSQSType(typ)
	awsSqs.Type = typStr

	return DestinationCreate{
		DestinationCreateAWSSQS: &awsSqs,
		Type:                    typ,
	}
}

func CreateDestinationCreateRabbitmq(rabbitmq DestinationCreateRabbitMQ) DestinationCreate {
	typ := DestinationCreateTypeRabbitmq

	typStr := DestinationCreateRabbitMQType(typ)
	rabbitmq.Type = typStr

	return DestinationCreate{
		DestinationCreateRabbitMQ: &rabbitmq,
		Type:                      typ,
	}
}

func CreateDestinationCreateHookdeck(hookdeck DestinationCreateHookdeck) DestinationCreate {
	typ := DestinationCreateTypeHookdeck

	typStr := DestinationCreateHookdeckType(typ)
	hookdeck.Type = typStr

	return DestinationCreate{
		DestinationCreateHookdeck: &hookdeck,
		Type:                      typ,
	}
}

func CreateDestinationCreateAwsKinesis(awsKinesis DestinationCreateAWSKinesis) DestinationCreate {
	typ := DestinationCreateTypeAwsKinesis

	typStr := DestinationCreateAWSKinesisType(typ)
	awsKinesis.Type = typStr

	return DestinationCreate{
		DestinationCreateAWSKinesis: &awsKinesis,
		Type:                        typ,
	}
}

func CreateDestinationCreateAzureServicebus(azureServicebus DestinationCreateAzureServiceBus) DestinationCreate {
	typ := DestinationCreateTypeAzureServicebus

	typStr := DestinationCreateAzureServiceBusType(typ)
	azureServicebus.Type = typStr

	return DestinationCreate{
		DestinationCreateAzureServiceBus: &azureServicebus,
		Type:                             typ,
	}
}

func (u *DestinationCreate) UnmarshalJSON(data []byte) error {

	type discriminator struct {
		Type string `json:"type"`
	}

	dis := new(discriminator)
	if err := json.Unmarshal(data, &dis); err != nil {
		return fmt.Errorf("could not unmarshal discriminator: %w", err)
	}

	switch dis.Type {
	case "webhook":
		destinationCreateWebhook := new(DestinationCreateWebhook)
		if err := utils.UnmarshalJSON(data, &destinationCreateWebhook, "", true, false); err != nil {
			return fmt.Errorf("could not unmarshal `%s` into expected (Type == webhook) type DestinationCreateWebhook within DestinationCreate: %w", string(data), err)
		}

		u.DestinationCreateWebhook = destinationCreateWebhook
		u.Type = DestinationCreateTypeWebhook
		return nil
	case "aws_sqs":
		destinationCreateAWSSQS := new(DestinationCreateAWSSQS)
		if err := utils.UnmarshalJSON(data, &destinationCreateAWSSQS, "", true, false); err != nil {
			return fmt.Errorf("could not unmarshal `%s` into expected (Type == aws_sqs) type DestinationCreateAWSSQS within DestinationCreate: %w", string(data), err)
		}

		u.DestinationCreateAWSSQS = destinationCreateAWSSQS
		u.Type = DestinationCreateTypeAwsSqs
		return nil
	case "rabbitmq":
		destinationCreateRabbitMQ := new(DestinationCreateRabbitMQ)
		if err := utils.UnmarshalJSON(data, &destinationCreateRabbitMQ, "", true, false); err != nil {
			return fmt.Errorf("could not unmarshal `%s` into expected (Type == rabbitmq) type DestinationCreateRabbitMQ within DestinationCreate: %w", string(data), err)
		}

		u.DestinationCreateRabbitMQ = destinationCreateRabbitMQ
		u.Type = DestinationCreateTypeRabbitmq
		return nil
	case "hookdeck":
		destinationCreateHookdeck := new(DestinationCreateHookdeck)
		if err := utils.UnmarshalJSON(data, &destinationCreateHookdeck, "", true, false); err != nil {
			return fmt.Errorf("could not unmarshal `%s` into expected (Type == hookdeck) type DestinationCreateHookdeck within DestinationCreate: %w", string(data), err)
		}

		u.DestinationCreateHookdeck = destinationCreateHookdeck
		u.Type = DestinationCreateTypeHookdeck
		return nil
	case "aws_kinesis":
		destinationCreateAWSKinesis := new(DestinationCreateAWSKinesis)
		if err := utils.UnmarshalJSON(data, &destinationCreateAWSKinesis, "", true, false); err != nil {
			return fmt.Errorf("could not unmarshal `%s` into expected (Type == aws_kinesis) type DestinationCreateAWSKinesis within DestinationCreate: %w", string(data), err)
		}

		u.DestinationCreateAWSKinesis = destinationCreateAWSKinesis
		u.Type = DestinationCreateTypeAwsKinesis
		return nil
	case "azure_servicebus":
		destinationCreateAzureServiceBus := new(DestinationCreateAzureServiceBus)
		if err := utils.UnmarshalJSON(data, &destinationCreateAzureServiceBus, "", true, false); err != nil {
			return fmt.Errorf("could not unmarshal `%s` into expected (Type == azure_servicebus) type DestinationCreateAzureServiceBus within DestinationCreate: %w", string(data), err)
		}

		u.DestinationCreateAzureServiceBus = destinationCreateAzureServiceBus
		u.Type = DestinationCreateTypeAzureServicebus
		return nil
	}

	return fmt.Errorf("could not unmarshal `%s` into any supported union types for DestinationCreate", string(data))
}

func (u DestinationCreate) MarshalJSON() ([]byte, error) {
	if u.DestinationCreateWebhook != nil {
		return utils.MarshalJSON(u.DestinationCreateWebhook, "", true)
	}

	if u.DestinationCreateAWSSQS != nil {
		return utils.MarshalJSON(u.DestinationCreateAWSSQS, "", true)
	}

	if u.DestinationCreateRabbitMQ != nil {
		return utils.MarshalJSON(u.DestinationCreateRabbitMQ, "", true)
	}

	if u.DestinationCreateHookdeck != nil {
		return utils.MarshalJSON(u.DestinationCreateHookdeck, "", true)
	}

	if u.DestinationCreateAWSKinesis != nil {
		return utils.MarshalJSON(u.DestinationCreateAWSKinesis, "", true)
	}

	if u.DestinationCreateAzureServiceBus != nil {
		return utils.MarshalJSON(u.DestinationCreateAzureServiceBus, "", true)
	}

	return nil, errors.New("could not marshal union type DestinationCreate: all fields are null")
}
