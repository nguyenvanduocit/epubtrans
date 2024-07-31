package main

import (
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/cmd"
	"os"
)

func main() {
	if err := cmd.Root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
