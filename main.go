package main

import (
	"fmt"
	"os"

	"github.com/juju/cmd"
)

func main() {
	ctx, err := cmd.DefaultContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create command context: %v", err)
		os.Exit(1)
	}
	exitcode := cmd.Main(newExtMigrateCommand(), ctx, os.Args[1:])
	os.Exit(exitcode)
}
