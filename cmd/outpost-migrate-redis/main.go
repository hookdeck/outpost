package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	app := NewCommand()

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
