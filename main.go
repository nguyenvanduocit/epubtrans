package main

import (
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/cmd"
	"log/slog"
	"os"
)

// those variables will be set by the build script to the correct values
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.Root.Version = fmt.Sprintf("%s, commit %s, built at %s", version, commit, date)
	if err := cmd.Root.Execute(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
