package config

import (
	"fmt"
)

const (
	ServiceTypeSingular ServiceType = iota
	ServiceTypeAPI
	ServiceTypeLog
	ServiceTypeDelivery
)

type ServiceType int

func (s ServiceType) String() string {
	switch s {
	case ServiceTypeSingular:
		return ""
	case ServiceTypeAPI:
		return "api"
	case ServiceTypeLog:
		return "log"
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
	case "log":
		return ServiceTypeLog, nil
	case "delivery":
		return ServiceTypeDelivery, nil
	}
	return ServiceType(-1), fmt.Errorf("unknown service: %s", s)
}
