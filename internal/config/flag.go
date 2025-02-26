package config

import "flag"

var (
	service      string
	config       string
	printVersion bool
)

func init() {
	flag.StringVar(&service, "service", "", "service (e.g. api, delivery, log). If empty, all services will run.")
	flag.StringVar(&config, "config", "", "config file (e.g. .env, config.yaml)")
	flag.BoolVar(&printVersion, "version", false, "print version information")
}

type Flags struct {
	Service string
	Config  string // Config file path
	Version bool   // Print version information
}

func ParseFlags() Flags {
	flag.Parse()
	return Flags{
		Service: service,
		Config:  config,
		Version: printVersion,
	}
}
