package config

import "flag"

var (
	service string
	config  string
)

func init() {
	flag.StringVar(&service, "service", "", "service (e.g. api, delivery, log). If empty, all services will run.")
	flag.StringVar(&config, "config", "", "config file (e.g. .env, config.yaml)")
}

type Flags struct {
	Service string
	Config  string // Config file path
}

func ParseFlags() Flags {
	flag.Parse()
	return Flags{
		Service: service,
		Config:  config,
	}
}
