package cmd

import (
	"context"
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/pkg/loader"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"path"
	"regexp"
	"syscall"
)

var Styling = &cobra.Command{
	Use:   "styling [unpackedEpubPath]",
	Short: "styling the content of an unpacked EPUB",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires exactly 1 arg(s), only received %d", len(args))
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
	Hide string
}

func init() {
	Styling.Flags().String("hide", "none", "hide source or target language")
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

	styleOptions := StylingOptions{
		Hide: cmd.Flag("hide").Value.String(),
	}

	return stylingEpub(ctx, unzipPath, styleOptions)
}

func stylingEpub(ctx context.Context, unzipPath string, styleOptions StylingOptions) error {
	container, err := loader.ParseContainer(unzipPath)
	if err != nil {
		return fmt.Errorf("failed to parse container: %w", err)
	}

	containerFileAbsPath := path.Join(unzipPath, container.Rootfile.FullPath)
	pkg, err := loader.ParsePackage(containerFileAbsPath)
	if err != nil {
		return fmt.Errorf("failed to parse package: %w", err)
	}

	contentDir := path.Dir(containerFileAbsPath)

	for _, item := range pkg.Manifest.Items {
		select {
		case <-ctx.Done():
			fmt.Println("Context cancelled, writing remaining content...")
			return nil
		default:
			if item.MediaType != "application/xhtml+xml" {
				continue
			}

			filePath := path.Join(contentDir, item.Href)
			fmt.Printf("Processing file: %s\n", item.Href)

			if err := stylingFile(ctx, filePath, styleOptions); err != nil {
				fmt.Printf("Error processing %s: %v\n", item.Href, err)
			}
		}
	}

	return nil
}

func stylingFile(ctx context.Context, filePath string, styleOptions StylingOptions) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	styleContent := `[` + util.ContentIdKey + `] { opacity: 0.7;}`

	if styleOptions.Hide == "source" {
		styleContent += `[` + util.ContentIdKey + `] { display: none !important; }`
	} else if styleOptions.Hide == "target" {
		styleContent = `[` + util.TranslationIdKey + `] { display: none !important; }`
	}

	// Prepare the style tag to inject
	styleTag := fmt.Sprintf("<style id=\"injected-style\">\n%s\n</style>", styleContent)

	// Check if the style tag with id "injected-style" already exists
	styleTagRegex := regexp.MustCompile(`<style\s+id="injected-style".*?>[\s\S]*?</style>`)
	var newContent []byte

	if loc := styleTagRegex.FindIndex(content); loc != nil {
		// Replace existing style tag
		newContent = append(content[:loc[0]], append([]byte(styleTag), content[loc[1]:]...)...)
		fmt.Printf("Replaced existing style tag in %s\n", filePath)
	} else {
		// Find the position to inject the style tag (after <head> or before </head>)
		headOpenRegex := regexp.MustCompile(`<head.*?>`)
		headCloseRegex := regexp.MustCompile(`</head>`)

		if loc := headOpenRegex.FindIndex(content); loc != nil {
			// Inject after <head>
			newContent = append(content[:loc[1]], append([]byte("\n"+styleTag+"\n"), content[loc[1]:]...)...)
		} else if loc := headCloseRegex.FindIndex(content); loc != nil {
			// Inject before </head>
			newContent = append(content[:loc[0]], append([]byte("\n"+styleTag+"\n"), content[loc[0]:]...)...)
		} else {
			return fmt.Errorf("no <head> tag found in %s", filePath)
		}
	}

	// Write the modified content back to the file
	err = os.WriteFile(filePath, newContent, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	fmt.Printf("Successfully injected or replaced style in %s\n", filePath)
	return nil
}
