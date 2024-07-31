package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/nguyenvanduocit/epubtrans/pkg/loader"
	"github.com/nguyenvanduocit/epubtrans/pkg/translator"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"
)

var Translate = &cobra.Command{
	Use:   "translate [unpackedEpubPath]",
	Short: "Translate the content of an unpacked EPUB",
	Args:  cobra.ExactArgs(1),
	RunE:  runTranslate,
}

func runTranslate(cmd *cobra.Command, args []string) error {
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

	return translateEpub(ctx, unzipPath)
}

func translateEpub(ctx context.Context, unzipPath string) error {
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
	limiter := rate.NewLimiter(rate.Every(time.Minute/50), 1)

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

			if err := processFile(ctx, filePath, limiter); err != nil {
				fmt.Printf("Error processing %s: %v\n", item.Href, err)
			}
		}
	}

	return nil
}

func processFile(ctx context.Context, filePath string, limiter *rate.Limiter) error {
	doc, err := openAndReadFile(filePath)
	if err != nil {
		return err
	}

	ensureUTF8Charset(doc)

	selector := fmt.Sprintf("[%s]:not([%s])", util.ContentIdKey, util.TranslationByIdKey)
	needToBeTranslateEls := doc.Find(selector)

	if needToBeTranslateEls.Length() == 0 {
		return nil
	}

	var modified bool
	done := make(chan struct{})

	go func() {
		needToBeTranslateEls.Each(func(i int, contentEl *goquery.Selection) {
			if translated := translateElement(ctx, i, contentEl, needToBeTranslateEls.Length(), limiter); translated {
				modified = true
			}

			if modified {
				if err := writeContentToFile(filePath, doc); err != nil {
					fmt.Printf("Error writing to file: %v\n", err)
				}
				modified = false
			}
		})
		close(done)
	}()

	select {
	case <-ctx.Done():
		if modified {
			if err := writeContentToFile(filePath, doc); err != nil {
				fmt.Printf("Error writing to file: %v\n", err)
			}
		}
		return ctx.Err()
	case <-done:
		return nil
	}
}

func openAndReadFile(filePath string) (*goquery.Document, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return goquery.NewDocumentFromReader(file)
}

func ensureUTF8Charset(doc *goquery.Document) {
	charset, _ := doc.Find("meta[charset]").Attr("charset")
	if charset != "utf-8" {
		doc.Find("head").AppendHtml(`<meta charset="utf-8">`)
	}
}

func translateElement(ctx context.Context, i int, contentEl *goquery.Selection, totalElements int, limiter *rate.Limiter) bool {
	if ctx.Err() != nil {
		return false
	}

	// Wait for rate limiter
	if err := limiter.Wait(ctx); err != nil {
		fmt.Printf("Rate limiter error: %v\n", err)
		return false
	}

	contentID := contentEl.AttrOr(util.ContentIdKey, "")

	htmlToTranslate, err := contentEl.Html()
	if err != nil || len(htmlToTranslate) <= 1 {
		return false
	}

	anthropicTranslator, err := translator.GetAnthropicTranslator(nil)
	if err != nil {
		fmt.Printf("Error getting translator: %v\n", err)
		return false
	}
	translatedContent, err := anthropicTranslator.Translate(ctx, htmlToTranslate, "English", "Vietnamese")
	if err != nil {
		fmt.Printf("Translation error: %v\n", err)

		if errors.Is(err, translator.ErrRateLimitExceeded) {
			return false
		}
		return false
	}

	if translatedContent == htmlToTranslate {
		contentEl.RemoveAttr(util.ContentIdKey)
		return false
	}

	if len(translatedContent) > len(htmlToTranslate)*4 {
		fmt.Printf("\t\tTranslation is not good: %s\n", contentID)
		fmt.Printf("\t\tTranslation: %s\n", translatedContent)
		return false
	}

	if err = manipulateHTML(contentEl, htmlToTranslate, translatedContent); err != nil {
		fmt.Printf("HTML manipulation error: %v\n", err)
		return false
	}

	fmt.Printf("\t%d/%d: %s\n", i+1, totalElements, contentID)

	return true
}

func manipulateHTML(doc *goquery.Selection, htmlContent, translatedContent string) error {
	translationID, err := generateContentID([]byte(translatedContent))
	if err != nil {
		return err
	}

	translatedElement := doc.Clone()
	translatedElement.RemoveAttr(util.ContentIdKey)
	translatedElement.SetHtml(translatedContent)
	translatedElement.SetAttr(util.TranslationIdKey, translationID)
	doc.SetAttr(util.TranslationByIdKey, translationID)
	doc.AfterSelection(translatedElement)

	return nil
}

func writeContentToFile(filePath string, doc *goquery.Document) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	html, err := doc.Html()
	if err != nil {
		return err
	}

	_, err = file.WriteString(html)
	return err
}
