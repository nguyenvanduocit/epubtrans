package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/nguyenvanduocit/epubtrans/pkg/processor"
	"github.com/nguyenvanduocit/epubtrans/pkg/translator"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	sourceLanguage string
	targetLanguage string
)

var Translate = &cobra.Command{
	Use:     "translate [unpackedEpubPath]",
	Short:   "Translate the content of an unpacked EPUB",
	Long:    "Translate the content of an unpacked EPUB using the Anthropic API",
	Example: `epubtrans translate path/to/unpacked/epub --source "English" --target "Vietnamese" --workers 1`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("unpackedEpubPath is required")
		}

		return util.ValidateEpubPath(args[0])
	},
	RunE: runTranslate,
}

func init() {
	Translate.Flags().StringVar(&sourceLanguage, "source", "English", "source language")
	Translate.Flags().StringVar(&targetLanguage, "target", "Vietnamese", "target language")
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

	if err := util.ValidateEpubPath(unzipPath); err != nil {
		return err
	}

	limiter := rate.NewLimiter(rate.Every(time.Minute/50), 1)

	return processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      1,
		JobBuffer:    1,
		ResultBuffer: 10,
	}, func(ctx context.Context, filePath string) error {
		return translateFile(ctx, filePath, limiter)
	})
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
	needToBeTranslateEls.Each(func(i int, contentEl *goquery.Selection) {
		select {
		case <-ctx.Done():
			return
		default:
			if translated := translateElement(ctx, i, contentEl, needToBeTranslateEls.Length(), limiter); translated {
				modified = true
			}
		}
	})

	if modified {
		return writeContentToFile(filePath, doc)
	}

	return nil
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
		fmt.Printf("\t\tError getting translator: %v\n", err)
		return false
	}
	translatedContent, err := anthropicTranslator.Translate(ctx, htmlToTranslate, sourceLanguage, targetLanguage)
	if err != nil {
		fmt.Printf("\t\tTranslation error: %v\n", err)

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

	if err = manipulateHTML(contentEl, targetLanguage, translatedContent); err != nil {
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
