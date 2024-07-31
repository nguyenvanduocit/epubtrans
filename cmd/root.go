package cmd

import (
	"github.com/nguyenvanduocit/epubtrans/cmd/clean"
	"github.com/spf13/cobra"
)

var Root = &cobra.Command{
	Use: "translate",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	Root.AddCommand(clean.Clean)
	Root.AddCommand(Unpack)
	Root.AddCommand(Mark)
	Root.AddCommand(Pack)
	Root.AddCommand(Translate)
}
