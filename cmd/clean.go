package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/nguyenvanduocit/epubtrans/pkg/processor"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
)

var Clean = &cobra.Command{
	Use:     "clean [unpackedEpubPath]",
	Short:   "Clean the html files",
	Long:    "Clean the html files by removing empty anchor and div tags",
	Example: "epubtrans clean path/to/unpacked/epub",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("unpackedEpubPath is required")
		}

		return util.ValidateEpubPath(args[0])
	},
	Version: "0.1.0",
	RunE:    runCleaner,
}

type CleaningOperation func(string) string

func init() {
	Clean.Flags().Int("workers", runtime.NumCPU(), "Number of worker goroutines")
}

func runCleaner(cmd *cobra.Command, args []string) error {
	unzipPath := args[0]
	ctx := cmd.Context()

	workers, _ := cmd.Flags().GetInt("workers")

	if err := util.ValidateEpubPath(unzipPath); err != nil {
		return err
	}

	cleaningOps := []CleaningOperation{
		removeEmptyAnchor,
		removeEmptyDiv,
	}

	return processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      workers,
		JobBuffer:    10,
		ResultBuffer: 10,
	}, func(ctx context.Context, filePath string) error {
		return cleanFile(ctx, filePath, cleaningOps)
	})
}

func cleanFile(ctx context.Context, filePath string, cleaningOps []CleaningOperation) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	cleanedContent := string(content)
	for _, op := range cleaningOps {
		cleanedContent = op(cleanedContent)
	}

	if cleanedContent != string(content) {
		err = os.WriteFile(filePath, []byte(cleanedContent), 0644)
		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fmt.Printf("Cleaned file: %s\n", filepath.Base(filePath))
	} else {
		fmt.Printf("No changes needed for file: %s\n", filepath.Base(filePath))
	}

	return nil
}

func removeEmptyAnchor(htmlContent string) string {
	regexPattern := regexp.MustCompile(`<a[^>]*(?:/>|>[\s\n]*</a>)`)
	return regexPattern.ReplaceAllString(htmlContent, "")
}

func removeEmptyDiv(htmlContent string) string {
	regexPattern := regexp.MustCompile(`<div[^>]*>[\s\n]*</div>`)
	return regexPattern.ReplaceAllString(htmlContent, "")
}
