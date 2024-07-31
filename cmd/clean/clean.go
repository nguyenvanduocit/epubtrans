package clean

import (
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/pkg/loader"
	"io"
	"os"
	"path"
	"regexp"
	"sync"

	"github.com/spf13/cobra"
)

var Clean = &cobra.Command{
	Use:   "clean",
	Short: "Clean the html files",
	Args:  cobra.ExactArgs(1),
	RunE:  runExtractor,
}

type CleaningOperation func(string) string

func runExtractor(cmd *cobra.Command, args []string) error {
	unzipPath := args[0]

	if _, err := os.Stat(unzipPath); os.IsNotExist(err) {
		return fmt.Errorf("unzip path %s does not exist", unzipPath)
	}

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

	xhtmlFiles := filterXHTMLFiles(pkg.Manifest.Items)
	totalFiles := len(xhtmlFiles)

	cmd.Printf("Total XHTML files to process: %d\n", totalFiles)

	cleaningOps := []CleaningOperation{
		removeEmptyAnchor,
		// Add more cleaning operations here as needed
	}

	results := processFilesParallel(cmd, contentDir, xhtmlFiles, cleaningOps)

	successCount := 0
	for _, result := range results {
		if result.err != nil {
			cmd.PrintErrf("Error processing %s: %v\n", result.filePath, result.err)
		} else {
			successCount++
		}
	}

	cmd.Printf("Processed %d out of %d files successfully.\n", successCount, totalFiles)
	return nil
}

func filterXHTMLFiles(items []loader.Item) []loader.Item {
	xhtmlFiles := make([]loader.Item, 0)
	for _, item := range items {
		if item.MediaType == "application/xhtml+xml" {
			xhtmlFiles = append(xhtmlFiles, item)
		}
	}
	return xhtmlFiles
}

type processResult struct {
	filePath string
	err      error
}

func processFilesParallel(cmd *cobra.Command, contentDir string, files []loader.Item, cleaningOps []CleaningOperation) []processResult {
	results := make([]processResult, len(files))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Limit concurrency to 10 goroutines

	for i, item := range files {
		wg.Add(1)
		go func(i int, item loader.Item) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			filePath := path.Join(contentDir, item.Href)
			cmd.Printf("Processing file %d of %d: %s\n", i+1, len(files), item.Href)

			err := processFile(filePath, cleaningOps)
			results[i] = processResult{filePath: filePath, err: err}
		}(i, item)
	}

	wg.Wait()
	return results
}

func processFile(filePath string, cleaningOps []CleaningOperation) error {
	content, err := readFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	for _, op := range cleaningOps {
		content = op(content)
	}

	return writeFile(filePath, content)
}

func readFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func writeFile(filePath, content string) error {
	return os.WriteFile(filePath, []byte(content), 0644)
}

func removeEmptyAnchor(htmlContent string) string {
	regexPattern := regexp.MustCompile(`<a [^>]*?\/>`)
	return regexPattern.ReplaceAllString(htmlContent, "")
}
