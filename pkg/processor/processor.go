package processor

import (
	"context"
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/pkg/loader"
	"golang.org/x/sync/errgroup"
	"path/filepath"
	"regexp"
)

// Config holds the configuration for the EPUB processor
type Config struct {
	Workers      int
	JobBuffer    int
	ResultBuffer int
}

// EpubItemProcessor is a function type for processing individual EPUB items
type EpubItemProcessor func(ctx context.Context, filePath string) error

// ProcessEpub processes an EPUB file with the given configuration and processor
func ProcessEpub(ctx context.Context, unzipPath string, cfg Config, processor EpubItemProcessor) error {
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

	jobs := make(chan string, cfg.JobBuffer)
	results := make(chan error, cfg.ResultBuffer)

	g, ctx := errgroup.WithContext(ctx)

	// Start worker pool
	for w := 0; w < cfg.Workers; w++ {
		g.Go(func() error {
			return worker(ctx, jobs, results, processor)
		})
	}

	// Feed jobs
	go func() {
		defer close(jobs)
		for _, item := range pkg.Manifest.Items {
			if item.MediaType != "application/xhtml+xml" {
				continue
			}

			if ShouldExcludeFile(item.Href) {
				fmt.Printf("Excluded file: %s\n", item.Href)
				continue
			}
			filePath := filepath.Join(contentDir, item.Href)
			select {
			case jobs <- filePath:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Collect results
	go func() {
		g.Wait()
		close(results)
	}()

	var processingErrors []error
	for err := range results {
		if err != nil {
			processingErrors = append(processingErrors, err)
		}
	}

	if err := g.Wait(); err != nil {
		return err
	}

	if len(processingErrors) > 0 {
		return fmt.Errorf("encountered %d errors during processing", len(processingErrors))
	}

	return nil
}

func worker(ctx context.Context, jobs <-chan string, results chan<- error, processor EpubItemProcessor) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case filePath, ok := <-jobs:
			if !ok {
				return nil
			}
			results <- processor(ctx, filePath)
		}
	}
}

var excludeRegex = regexp.MustCompile(`(?i)(preface|introduction|foreword|prologue|toc|table\s*of\s*contents|title|cover|copyright|colophon|dedication|acknowledgements?|about\s*the\s*author|bibliography|glossary|index|appendix|notes?|footnotes?|endnotes?|references|epub-meta|metadata|nav|ncx|opf|front\s*matter|back\s*matter|halftitle|frontispiece|epigraph|list\s*of\s*(figures|tables|illustrations)|copyright\s*page|series\s*page|reviews|praise|also\s*by\s*the\s*author|author\s*bio|publication\s*info|imprint|credits|permissions|disclaimer|errata|synopsis|summary)`)

// ShouldExcludeFile determines if a file should be excluded based on its name
func ShouldExcludeFile(fileName string) bool {
	return excludeRegex.MatchString(fileName)
}
