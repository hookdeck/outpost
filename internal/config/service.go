package config

import (
	"errors"
	"fmt"
)

const (
	ServiceTypeSingular ServiceType = iota
	ServiceTypeAPI
	ServiceTypeData
	ServiceTypeDelivery
)

type ServiceType int

func (s ServiceType) String() string {
	switch s {
	case ServiceTypeSingular:
		return ""
	case ServiceTypeAPI:
		return "api"
	case ServiceTypeData:
		return "data"
	case ServiceTypeDelivery:
		return "delievery"
	}
	return "unknown"
}

func ServiceTypeFromString(s string) (ServiceType, error) {
	switch s {
	case "":
		return ServiceTypeSingular, nil
	case "api":
		return ServiceTypeAPI, nil
	case "data":
		return ServiceTypeData, nil
	case "delivery":
		return ServiceTypeDelivery, nil
	}
	return ServiceType(-1), errors.New(fmt.Sprintf("unknown service: %s", s))
}
