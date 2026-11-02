package main

import (
	"fmt"
	"os"

	"torn/internal/cli"
)

func main() {
	app, err := cli.NewApp(os.Stdout, os.Stderr, "", nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing torn: %v\n", err)
		os.Exit(1)
	}

	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
