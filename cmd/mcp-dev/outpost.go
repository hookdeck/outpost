package main

import (
	"net/http"
	"strings"

	"github.com/urfave/cli/v3"
)

// conn is the connection to the Outpost API shared by serve, check and tour.
type conn struct {
	URL    string
	APIKey string
	Header http.Header
}

func outpostFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "outpost-url", Value: "http://localhost:3333/api/v1", Sources: cli.EnvVars("OUTPOST_URL"),
			Usage: "Outpost API base URL, including any path prefix"},
		&cli.StringFlag{Name: "api-key", Value: "apikey", Sources: cli.EnvVars("OUTPOST_API_KEY"),
			Usage: "sent as Authorization: Bearer"},
		&cli.StringSliceFlag{Name: "outpost-header", Sources: cli.EnvVars("OUTPOST_HEADERS"),
			Usage: "extra request header 'Name: value', repeatable (OUTPOST_HEADERS: comma-separated)"},
	}
}

func outpostConn(cmd *cli.Command) (conn, error) {
	c := conn{
		URL:    strings.TrimRight(cmd.String("outpost-url"), "/"),
		APIKey: cmd.String("api-key"),
		Header: http.Header{},
	}
	for _, h := range cmd.StringSlice("outpost-header") {
		name, value, err := parseHeader(h)
		if err != nil {
			return c, err
		}
		c.Header.Add(name, value)
	}
	return c, nil
}

// parseHeader splits "Name: value" at the first colon.
func parseHeader(s string) (string, string, error) {
	name, value, ok := strings.Cut(s, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !ok || name == "" {
		return "", "", usagef("--outpost-header %q: want 'Name: value'", s)
	}
	return name, value, nil
}
