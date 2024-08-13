package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/liushuangls/go-anthropic/v2"
	"github.com/nguyenvanduocit/epubtrans/pkg/processor"
	"github.com/nguyenvanduocit/epubtrans/pkg/translator"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
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
	Example: `epubtrans translate path/to/unpacked/epub --source "English" --target "Vietnamese"`,
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
	Translate.Flags().String("model", anthropic.ModelClaude3Dot5Sonnet20240620, "Anthropic model to use")
}

type elementToTranslate struct {
	filePath      string
	contentEl     *goquery.Selection
	doc           *goquery.Document
	totalElements int
	index         int
}

var fileLocks = make(map[string]*sync.Mutex)
var fileLocksLock sync.Mutex

func getFileLock(filePath string) *sync.Mutex {
	fileLocksLock.Lock()
	defer fileLocksLock.Unlock()

	if lock, exists := fileLocks[filePath]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	fileLocks[filePath] = lock
	return lock
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

	limiter := rate.NewLimiter(rate.Every(time.Minute/50), 10)

	anthropicTranslator, err := translator.GetAnthropicTranslator(&translator.Config{
		APIKey:      os.Getenv("ANTHROPIC_KEY"),
		Model:       cmd.Flag("model").Value.String(),
		Temperature: 0.3,
		MaxTokens:   4096,
	})
	if err != nil {
		return fmt.Errorf("error getting translator: %v", err)
	}

	// we can handle 10 elements at a time
	elementChan := make(chan elementToTranslate, 10)
	doneChan := make(chan struct{})

	go processElements(ctx, elementChan, doneChan, anthropicTranslator, limiter)

	// 1 worker and 1 job at a time, mean 1 file at a time
	err = processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      1,
		JobBuffer:    1,
		ResultBuffer: 10,
	}, func(ctx context.Context, filePath string) error {
		return processFile(ctx, filePath, elementChan)
	})

	close(elementChan)
	<-doneChan

	return err
}

func processElements(ctx context.Context, elementChan <-chan elementToTranslate, doneChan chan<- struct{}, anthropicTranslator translator.Translator, limiter *rate.Limiter) {
	defer close(doneChan)

	for element := range elementChan {
		select {
		case <-ctx.Done():
			return
		default:
			fmt.Printf("Processing %s:%s\n", element.filePath, element.contentEl.AttrOr(util.ContentIdKey, ""))
			if translated := translateElement(ctx, element, anthropicTranslator, limiter); translated {
				fileLock := getFileLock(element.filePath)
				fileLock.Lock()
				if err := writeContentToFile(element.filePath, element.doc); err != nil {
					fmt.Printf("Error writing to file: %v\n", err)
				}
				fileLock.Unlock()
			}
		}
	}
}

func processFile(ctx context.Context, filePath string, elementChan chan<- elementToTranslate) error {
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

	needToBeTranslateEls.Each(func(i int, contentEl *goquery.Selection) {
		select {
		case <-ctx.Done():
			return
		default:
			elementChan <- elementToTranslate{
				filePath:      filePath,
				contentEl:     contentEl,
				doc:           doc,
				totalElements: needToBeTranslateEls.Length(),
				index:         i,
			}
		}
	})

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

func translateElement(ctx context.Context, element elementToTranslate, anthropicTranslator translator.Translator, limiter *rate.Limiter) bool {
	if ctx.Err() != nil {
		return false
	}

	contentID := element.contentEl.AttrOr(util.ContentIdKey, "")
	htmlToTranslate, err := element.contentEl.Html()
	if err != nil || len(htmlToTranslate) <= 1 {
		return false
	}

	translatedContent, err := retryTranslate(ctx, anthropicTranslator, limiter, htmlToTranslate, sourceLanguage, targetLanguage)
	if err != nil {
		fmt.Printf("\t\tTranslation error: %v\n", err)
		return false
	}

	if !isTranslationValid(htmlToTranslate, translatedContent) {
		fmt.Printf("\t\tInvalid translation for: %s\n", contentID)
		return false
	}

	if err = manipulateHTML(element.contentEl, targetLanguage, translatedContent); err != nil {
		fmt.Printf("HTML manipulation error: %v\n", err)
		return false
	}

	return true
}

func retryTranslate(ctx context.Context, t translator.Translator, limiter *rate.Limiter, content, sourceLang, targetLang string) (string, error) {
	maxRetries := 3
	baseDelay := time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			// Wait for rate limiter
			if err := limiter.Wait(ctx); err != nil {
				return "", fmt.Errorf("rate limiter error: %w", err)
			}

			translatedContent, err := t.Translate(ctx, content, sourceLang, targetLang)
			if err == nil {
				return translatedContent, nil
			}

			if errors.Is(err, translator.ErrRateLimitExceeded) {
				// For rate limit errors, wait longer before retrying
				time.Sleep(calculateBackoff(attempt, baseDelay*10))
			} else {
				// For other errors, use normal backoff
				time.Sleep(calculateBackoff(attempt, baseDelay))
			}
		}
	}

	return "", fmt.Errorf("max retries reached")
}

func calculateBackoff(attempt int, baseDelay time.Duration) time.Duration {
	backoff := float64(baseDelay) * math.Pow(2, float64(attempt))
	jitter := rand.Float64() * float64(baseDelay)
	return time.Duration(backoff + jitter)
}

func isTranslationValid(original, translated string) bool {
	if translated == original {
		return false
	}

	if countWords(translated) > countWords(original)*3 {
		return false
	}

	if isHtmlDifferent(original, translated) {
		return false
	}

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
