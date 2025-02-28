package main

import (
	"fmt"
	"github.com/dutchsteven/epubtrans/cmd"
	"log/slog"
	"os"
)

// those variables will be set by the build script to the correct values
var (
	version = "v0.0.0"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.Root.Version = fmt.Sprintf("%s-c%s-b%s", version, commit, date)
	if err := cmd.Root.Execute(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
