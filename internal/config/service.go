package config

type ServiceType int

const (
	ServiceTypeAll ServiceType = iota
	ServiceTypeAPI
	ServiceTypeLog
	ServiceTypeDelivery
)

func (s ServiceType) String() string {
	switch s {
	case ServiceTypeAll:
		return "all"
	case ServiceTypeAPI:
		return "api"
	case ServiceTypeLog:
		return "log"
	case ServiceTypeDelivery:
		return "delivery"
	}
	return "unknown"
}

func ServiceTypeFromString(s string) (ServiceType, error) {
	switch s {
	case "":
		return ServiceTypeAll, nil
	case "all":
		return ServiceTypeAll, nil
	case "api":
		return ServiceTypeAPI, nil
	case "log":
		return ServiceTypeLog, nil
	case "delivery":
		return ServiceTypeDelivery, nil
	}
	return ServiceType(-1), ErrInvalidServiceType
}
