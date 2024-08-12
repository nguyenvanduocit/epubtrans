package cmd

import (
	"context"
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/pkg/processor"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"syscall"
)

var Styling = &cobra.Command{
	Use:     "styling [unpackedEpubPath]",
	Short:   "styling the content of an unpacked EPUB",
	Example: "epubtrans styling path/to/unpacked/epub",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("unpackedEpubPath is required")
		}

		if err := util.ValidateEpubPath(args[0]); err != nil {
			return err
		}

		hide, err := cmd.Flags().GetString("hide")
		if err != nil {
			return fmt.Errorf("failed to get hide flag: %w", err)
		}
		if hide != "source" && hide != "target" && hide != "none" {
			return fmt.Errorf("hide flag must be either 'source' or 'target'")
		}
		return nil
	},
	RunE: runStyling,
}

type StylingOptions struct {
	Hide    string
	Workers int
}

func init() {
	Styling.Flags().String("hide", "none", "hide source or target language")
	Styling.Flags().Int("workers", runtime.NumCPU(), "Number of worker goroutines")
}

func runStyling(cmd *cobra.Command, args []string) error {
	unzipPath := args[0]
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("Interrupt received, initiating graceful shutdown...")
		cancel()
	}()

	hide, _ := cmd.Flags().GetString("hide")
	workers, _ := cmd.Flags().GetInt("workers")

	styleOptions := StylingOptions{
		Hide:    hide,
		Workers: workers,
	}

	if err := util.ValidateEpubPath(unzipPath); err != nil {
		return err
	}

	return processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      workers,
		JobBuffer:    10,
		ResultBuffer: 10,
	}, func(ctx context.Context, filePath string) error {
		return stylingFile(ctx, filePath, styleOptions)
	})
}

func generateStyleContent(hide string) string {
	styleContent := fmt.Sprintf("[%s] { opacity: 0.7;}", util.ContentIdKey)

	switch hide {
	case "source":
		styleContent += fmt.Sprintf("[%s] { display: none !important; }", util.ContentIdKey)
	case "target":
		styleContent = fmt.Sprintf("[%s] { display: none !important; }", util.TranslationIdKey)
	}

	return styleContent
}

func injectOrReplaceStyle(content []byte, styleTag string) ([]byte, error) {
	styleTagRegex := regexp.MustCompile(`<style\s+id="injected-style".*?>[\s\S]*?</style>`)
	headOpenRegex := regexp.MustCompile(`<head.*?>`)
	headCloseRegex := regexp.MustCompile(`</head>`)

	if loc := styleTagRegex.FindIndex(content); loc != nil {
		// Replace existing style tag
		return append(content[:loc[0]], append([]byte(styleTag), content[loc[1]:]...)...), nil
	}

	if loc := headOpenRegex.FindIndex(content); loc != nil {
		// Inject after <head>
		return append(content[:loc[1]], append([]byte("\n"+styleTag+"\n"), content[loc[1]:]...)...), nil
	}

	if loc := headCloseRegex.FindIndex(content); loc != nil {
		// Inject before </head>
		return append(content[:loc[0]], append([]byte("\n"+styleTag+"\n"), content[loc[0]:]...)...), nil
	}

	return nil, fmt.Errorf("no <head> tag found")
}

func stylingFile(ctx context.Context, filePath string, styleOptions StylingOptions) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	styleContent := generateStyleContent(styleOptions.Hide)
	styleTag := fmt.Sprintf("<style id=\"injected-style\">\n%s\n</style>", styleContent)

	newContent, err := injectOrReplaceStyle(content, styleTag)
	if err != nil {
		return fmt.Errorf("failed to inject or replace style in %s: %w", filePath, err)
	}

	err = os.WriteFile(filePath, newContent, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	fmt.Printf("Successfully injected or replaced style in %s\n", filePath)
	return nil
}
