package cmd

import (
	"fmt"

	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
)

var Prepare = &cobra.Command{
	Use:   "prepare [epubPath]",
	Short: "Prepare an EPUB file by running unpack, clean, and mark commands",
	Long: `This command automates the preparation process for an EPUB file by running multiple commands in sequence:
1. Unpack - Extracts the EPUB contents
2. Clean - Removes unnecessary HTML elements
3. Mark - Adds unique identifiers to content nodes
4. Styling - Applies default styling`,
	Example: "epubtrans prepare path/to/book.epub",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("epubPath is required")
		}
		return nil
	},
	RunE: runPrepare,
}

func runPrepare(cmd *cobra.Command, args []string) error {
	epubPath := args[0]

	// 1. Run unpack command
	if err := Unpack.RunE(cmd, []string{epubPath}); err != nil {
		return fmt.Errorf("failed to unpack EPUB: %w", err)
	}

	// Get the unpacked directory path
	unpackedPath, err := util.GetUnzipDestination(epubPath)
	if err != nil {
		return fmt.Errorf("failed to determine unpacked directory path: %w", err)
	}

	// 2. Run clean command
	if err := Clean.RunE(cmd, []string{unpackedPath}); err != nil {
		return fmt.Errorf("failed to clean HTML files: %w", err)
	}

	// 3. Run mark command
	if err := Mark.RunE(cmd, []string{unpackedPath}); err != nil {
		return fmt.Errorf("failed to mark content: %w", err)
	}

	// 4. Run styling command
	if err := Styling.RunE(cmd, []string{unpackedPath}); err != nil {
		return fmt.Errorf("failed to apply styling: %w", err)
	}

	fmt.Printf("Successfully prepared EPUB at: %s\n", unpackedPath)
	return nil
}