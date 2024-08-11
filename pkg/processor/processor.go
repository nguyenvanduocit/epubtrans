package processor

import (
	"context"
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/pkg/loader"
	"path/filepath"
	"regexp"
	"sync"
)

type EpubItemProcessor func(ctx context.Context, filePath string) error

func worker(ctx context.Context, jobs <-chan string, results chan<- error, wg *sync.WaitGroup, processor EpubItemProcessor) {
	defer wg.Done()
	for filePath := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
			results <- processor(ctx, filePath)
		}
	}
}

func ProcessEpub(ctx context.Context, unzipPath string, workers int, processor EpubItemProcessor) error {
	container, err := loader.ParseContainer(unzipPath)
	if err != nil {
		return fmt.Errorf("failed to parse container: %w", err)
	}

	containerFileAbsPath := filepath.Join(unzipPath, container.Rootfile.FullPath)
	pkg, err := loader.ParsePackage(containerFileAbsPath)
	if err != nil {
		return fmt.Errorf("failed to parse package: %w", err)
	}

	contentDir := filepath.Dir(containerFileAbsPath)

	jobs := make(chan string, len(pkg.Manifest.Items))
	results := make(chan error, len(pkg.Manifest.Items))

	var wg sync.WaitGroup
	for w := 1; w <= workers; w++ {
		wg.Add(1)
		go worker(ctx, jobs, results, &wg, processor)
	}

	for _, item := range pkg.Manifest.Items {
		if item.MediaType != "application/xhtml+xml" {
			continue
		}

		if ShouldExcludeFile(item.Href) {
			fmt.Printf("Skipping file: %s (excluded from processing)\n", item.Href)
			continue
		}

		fmt.Println("Processing file:", item.Href)

		filePath := filepath.Join(contentDir, item.Href)
		jobs <- filePath
	}

	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	for err := range results {
		if err != nil {
			fmt.Printf("Error processing file: %v\n", err)
		}
	}

	return nil
}

var excludeRegex = regexp.MustCompile(`(?i)(preface|introduction|foreword|prologue|toc|table\s*of\s*contents|title|cover|copyright|colophon|dedication|acknowledgements?|about\s*the\s*author|bibliography|glossary|index|appendix|notes?|footnotes?|endnotes?|references|epub-meta|metadata|nav|ncx|opf|front\s*matter|back\s*matter|halftitle|frontispiece|epigraph|list\s*of\s*(figures|tables|illustrations)|copyright\s*page|series\s*page|reviews|praise\s*for|also\s*by\s*the\s*author|author\s*bio|publication\s*info|imprint|credits|permissions|disclaimer|errata|synopsis|summary)`)

func ShouldExcludeFile(fileName string) bool {
	// Check if the file name matches the exclude regex
	if excludeRegex.MatchString(fileName) {
		return true
	}

	return false
}
