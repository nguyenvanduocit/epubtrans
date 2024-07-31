package main

import (
	"epubtrans/cmd"
	"fmt"
	"os"
)

func main() {
	if err := cmd.Root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
