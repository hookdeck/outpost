// Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.

package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hookdeck/outpost/sdks/outpost-go/internal/utils"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

type ListTenantDestinationsGlobals struct {
	TenantID *string `pathParam:"style=simple,explode=false,name=tenant_id"`
}

func (o *ListTenantDestinationsGlobals) GetTenantID() *string {
	if o == nil {
		return nil
	}
	return o.TenantID
}

type ListTenantDestinationsTypeEnum2 string

const (
	ListTenantDestinationsTypeEnum2Webhook    ListTenantDestinationsTypeEnum2 = "webhook"
	ListTenantDestinationsTypeEnum2AwsSqs     ListTenantDestinationsTypeEnum2 = "aws_sqs"
	ListTenantDestinationsTypeEnum2Rabbitmq   ListTenantDestinationsTypeEnum2 = "rabbitmq"
	ListTenantDestinationsTypeEnum2Hookdeck   ListTenantDestinationsTypeEnum2 = "hookdeck"
	ListTenantDestinationsTypeEnum2AwsKinesis ListTenantDestinationsTypeEnum2 = "aws_kinesis"
)

func (e ListTenantDestinationsTypeEnum2) ToPointer() *ListTenantDestinationsTypeEnum2 {
	return &e
}
func (e *ListTenantDestinationsTypeEnum2) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch v {
	case "webhook":
		fallthrough
	case "aws_sqs":
		fallthrough
	case "rabbitmq":
		fallthrough
	case "hookdeck":
		fallthrough
	case "aws_kinesis":
		*e = ListTenantDestinationsTypeEnum2(v)
		return nil
	default:
		return fmt.Errorf("invalid value for ListTenantDestinationsTypeEnum2: %v", v)
	}
}

type ListTenantDestinationsTypeEnum1 string

const (
	ListTenantDestinationsTypeEnum1Webhook    ListTenantDestinationsTypeEnum1 = "webhook"
	ListTenantDestinationsTypeEnum1AwsSqs     ListTenantDestinationsTypeEnum1 = "aws_sqs"
	ListTenantDestinationsTypeEnum1Rabbitmq   ListTenantDestinationsTypeEnum1 = "rabbitmq"
	ListTenantDestinationsTypeEnum1Hookdeck   ListTenantDestinationsTypeEnum1 = "hookdeck"
	ListTenantDestinationsTypeEnum1AwsKinesis ListTenantDestinationsTypeEnum1 = "aws_kinesis"
)

func (e ListTenantDestinationsTypeEnum1) ToPointer() *ListTenantDestinationsTypeEnum1 {
	return &e
}
func (e *ListTenantDestinationsTypeEnum1) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch v {
	case "webhook":
		fallthrough
	case "aws_sqs":
		fallthrough
	case "rabbitmq":
		fallthrough
	case "hookdeck":
		fallthrough
	case "aws_kinesis":
		*e = ListTenantDestinationsTypeEnum1(v)
		return nil
	default:
		return fmt.Errorf("invalid value for ListTenantDestinationsTypeEnum1: %v", v)
	}
}

type TypeType string

const (
	TypeTypeListTenantDestinationsTypeEnum1        TypeType = "listTenantDestinations_type_enum_1"
	TypeTypeArrayOfListTenantDestinationsTypeEnum2 TypeType = "arrayOfListTenantDestinationsTypeEnum2"
)

// Type - Filter destinations by type(s).
type Type struct {
	ListTenantDestinationsTypeEnum1        *ListTenantDestinationsTypeEnum1  `queryParam:"inline"`
	ArrayOfListTenantDestinationsTypeEnum2 []ListTenantDestinationsTypeEnum2 `queryParam:"inline"`

	Type TypeType
}

func CreateTypeListTenantDestinationsTypeEnum1(listTenantDestinationsTypeEnum1 ListTenantDestinationsTypeEnum1) Type {
	typ := TypeTypeListTenantDestinationsTypeEnum1

	return Type{
		ListTenantDestinationsTypeEnum1: &listTenantDestinationsTypeEnum1,
		Type:                            typ,
	}
}

func CreateTypeArrayOfListTenantDestinationsTypeEnum2(arrayOfListTenantDestinationsTypeEnum2 []ListTenantDestinationsTypeEnum2) Type {
	typ := TypeTypeArrayOfListTenantDestinationsTypeEnum2

	return Type{
		ArrayOfListTenantDestinationsTypeEnum2: arrayOfListTenantDestinationsTypeEnum2,
		Type:                                   typ,
	}
}

func (u *Type) UnmarshalJSON(data []byte) error {

	var listTenantDestinationsTypeEnum1 ListTenantDestinationsTypeEnum1 = ListTenantDestinationsTypeEnum1("")
	if err := utils.UnmarshalJSON(data, &listTenantDestinationsTypeEnum1, "", true, true); err == nil {
		u.ListTenantDestinationsTypeEnum1 = &listTenantDestinationsTypeEnum1
		u.Type = TypeTypeListTenantDestinationsTypeEnum1
		return nil
	}

	var arrayOfListTenantDestinationsTypeEnum2 []ListTenantDestinationsTypeEnum2 = []ListTenantDestinationsTypeEnum2{}
	if err := utils.UnmarshalJSON(data, &arrayOfListTenantDestinationsTypeEnum2, "", true, true); err == nil {
		u.ArrayOfListTenantDestinationsTypeEnum2 = arrayOfListTenantDestinationsTypeEnum2
		u.Type = TypeTypeArrayOfListTenantDestinationsTypeEnum2
		return nil
	}

	return fmt.Errorf("could not unmarshal `%s` into any supported union types for Type", string(data))
}

func (u Type) MarshalJSON() ([]byte, error) {
	if u.ListTenantDestinationsTypeEnum1 != nil {
		return utils.MarshalJSON(u.ListTenantDestinationsTypeEnum1, "", true)
	}

	if u.ArrayOfListTenantDestinationsTypeEnum2 != nil {
		return utils.MarshalJSON(u.ArrayOfListTenantDestinationsTypeEnum2, "", true)
	}

	return nil, errors.New("could not marshal union type Type: all fields are null")
}

type TopicsType string

const (
	TopicsTypeStr        TopicsType = "str"
	TopicsTypeArrayOfStr TopicsType = "arrayOfStr"
)

// Topics - Filter destinations by supported topic(s).
type Topics struct {
	Str        *string  `queryParam:"inline"`
	ArrayOfStr []string `queryParam:"inline"`

	Type TopicsType
}

func CreateTopicsStr(str string) Topics {
	typ := TopicsTypeStr

	return Topics{
		Str:  &str,
		Type: typ,
	}
}

func CreateTopicsArrayOfStr(arrayOfStr []string) Topics {
	typ := TopicsTypeArrayOfStr

	return Topics{
		ArrayOfStr: arrayOfStr,
		Type:       typ,
	}
}

func (u *Topics) UnmarshalJSON(data []byte) error {

	var str string = ""
	if err := utils.UnmarshalJSON(data, &str, "", true, true); err == nil {
		u.Str = &str
		u.Type = TopicsTypeStr
		return nil
	}

	var arrayOfStr []string = []string{}
	if err := utils.UnmarshalJSON(data, &arrayOfStr, "", true, true); err == nil {
		u.ArrayOfStr = arrayOfStr
		u.Type = TopicsTypeArrayOfStr
		return nil
	}

	return fmt.Errorf("could not unmarshal `%s` into any supported union types for Topics", string(data))
}

func (u Topics) MarshalJSON() ([]byte, error) {
	if u.Str != nil {
		return utils.MarshalJSON(u.Str, "", true)
	}

	if u.ArrayOfStr != nil {
		return utils.MarshalJSON(u.ArrayOfStr, "", true)
	}

	return nil, errors.New("could not marshal union type Topics: all fields are null")
}

type ListTenantDestinationsRequest struct {
	// The ID of the tenant. Required when using AdminApiKey authentication.
	TenantID *string `pathParam:"style=simple,explode=false,name=tenant_id"`
	// Filter destinations by type(s).
	Type *Type `queryParam:"style=form,explode=true,name=type"`
	// Filter destinations by supported topic(s).
	Topics *Topics `queryParam:"style=form,explode=true,name=topics"`
}

func (o *ListTenantDestinationsRequest) GetTenantID() *string {
	if o == nil {
		return nil
	}
	return o.TenantID
}

func (o *ListTenantDestinationsRequest) GetType() *Type {
	if o == nil {
		return nil
	}
	return o.Type
}

func (o *ListTenantDestinationsRequest) GetTopics() *Topics {
	if o == nil {
		return nil
	}
	return o.Topics
}

type ListTenantDestinationsResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
	// A list of destinations.
	Destinations []components.Destination
}

func (o *ListTenantDestinationsResponse) GetHTTPMeta() components.HTTPMetadata {
	if o == nil {
		return components.HTTPMetadata{}
	}
	return o.HTTPMeta
}

func (o *ListTenantDestinationsResponse) GetDestinations() []components.Destination {
	if o == nil {
		return nil
	}
	return o.Destinations
}
