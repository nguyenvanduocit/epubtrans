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
	"github.com/spf13/viper"
	"golang.org/x/time/rate"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var Translate = &cobra.Command{
	Use:   "translate [unpackedEpubPath]",
	Short: "Translate the content of an unpacked EPUB",
	Args:  cobra.ExactArgs(1),
	RunE:  runTranslate,
}

func init() {
	Translate.Flags().String("source", "English", "source language")
	Translate.Flags().String("target", "Vietnamese", "target language")

	viper.BindPFlag("source", Translate.Flags().Lookup("source"))
	viper.BindPFlag("target", Translate.Flags().Lookup("target"))
}

var excludeRegex = regexp.MustCompile(`(?i)(preface|introduction|foreword|prologue|toc|table\s*of\s*contents|title|cover|copyright|colophon|dedication|acknowledgements?|about\s*the\s*author|bibliography|glossary|index|appendix|notes?|footnotes?|endnotes?|references|epub-meta|metadata|nav|ncx|opf|front\s*matter|back\s*matter|halftitle|frontispiece|epigraph|list\s*of\s*(figures|tables|illustrations)|copyright\s*page|series\s*page|reviews|praise\s*for|also\s*by\s*the\s*author|author\s*bio|publication\s*info|imprint|credits|permissions|disclaimer|errata|synopsis|summary)`)

func shouldExcludeFile(fileName string) bool {
	// Check if the file name matches the exclude regex
	if excludeRegex.MatchString(fileName) {
		return true
	}

	return false
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

			if shouldExcludeFile(item.Href) {
				fmt.Printf("Skipping file: %s (excluded from translation)\n", item.Href)
				continue
			}

			filePath := path.Join(contentDir, item.Href)
			fmt.Printf("Processing file: %s\n", item.Href)

			if err := translateFile(ctx, filePath, limiter); err != nil {
				fmt.Printf("Error processing %s: %v\n", item.Href, err)
			}
		}
	}

	return nil
}

func translateFile(ctx context.Context, filePath string, limiter *rate.Limiter) error {
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
	translatedContent, err := anthropicTranslator.Translate(ctx, htmlToTranslate, viper.GetString("source"), viper.GetString("target"))
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

	if countWords(translatedContent) > countWords(htmlToTranslate)*3 {
		fmt.Printf("\t\tTranslation is not good: %s\n", contentID)
		fmt.Printf("\t\tTranslation: %s\n", translatedContent)
		return false
	}

	if isHtmlDifferent(htmlToTranslate, translatedContent) {
		fmt.Printf("\t\tTranslation struct is not good: %s\n", contentID)
		fmt.Printf("\t\tTranslation: %s\n", translatedContent)
		return false
	}

	if err = manipulateHTML(contentEl, viper.GetString("target"), translatedContent); err != nil {
		fmt.Printf("HTML manipulation error: %v\n", err)
		return false
	}

	fmt.Printf("\t%d/%d: %s\n", i+1, totalElements, contentID)

	return true
}

func isHtmlDifferent(html1, html2 string) bool {
	doc1, err := goquery.NewDocumentFromReader(strings.NewReader(html1))
	if err != nil {
		return false
	}

	doc2, err := goquery.NewDocumentFromReader(strings.NewReader(html2))
	if err != nil {
		return false
	}

	// loop to compare
	return compareNodes(doc1.Contents(), doc2.Contents())
}

func compareNodes(nodes1, nodes2 *goquery.Selection) bool {
	if nodes1.Length() != nodes2.Length() {
		return true
	}

	for i := range nodes1.Nodes {
		node1 := nodes1.Eq(i)
		node2 := nodes2.Eq(i)

		if node1.Nodes[0].Type != node2.Nodes[0].Type {
			return true
		}

		if node1.Nodes[0].Type == 1 {
			if node1.Nodes[0].Data != node2.Nodes[0].Data {
				return true
			}

			if node1.Nodes[0].Attr != nil && node2.Nodes[0].Attr != nil {
				if len(node1.Nodes[0].Attr) != len(node2.Nodes[0].Attr) {
					return true
				}

				for j := range node1.Nodes[0].Attr {
					if node1.Nodes[0].Attr[j].Key != node2.Nodes[0].Attr[j].Key || node1.Nodes[0].Attr[j].Val != node2.Nodes[0].Attr[j].Val {
						return true
					}
				}
			}

			if compareNodes(node1.Contents(), node2.Contents()) {
				return true
			}
		}
	}

	return false
}

func countWords(s string) int {
	return len(strings.Fields(s))
}

func manipulateHTML(doc *goquery.Selection, targetLang, translatedContent string) error {
	translationID, err := generateContentID([]byte(translatedContent + targetLang))
	if err != nil {
		return err
	}

	translatedElement := doc.Clone()
	translatedElement.RemoveAttr(util.ContentIdKey)
	translatedElement.SetHtml(translatedContent)
	translatedElement.SetAttr(util.TranslationIdKey, translationID)
	translatedElement.SetAttr(util.TranslationLangKey, targetLang)

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
