package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/liushuangls/go-anthropic/v2"
	"github.com/nguyenvanduocit/epubtrans/pkg/loader"
	"github.com/nguyenvanduocit/epubtrans/pkg/processor"
	"github.com/nguyenvanduocit/epubtrans/pkg/translator"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"
)

var (
	sourceLanguage string
	targetLanguage string
)

var Translate = &cobra.Command{
	Use:   "translate [unpackedEpubPath]",
	Short: "Translate the content of an unpacked EPUB file",
	Long: `This command translates the content of an unpacked EPUB file using the Anthropic API. 
It allows you to specify the source and target languages for the translation. 
Make sure to provide the path to the unpacked EPUB directory and the desired languages.`,
	Example: `epubtrans translate path/to/unpacked/epub --source "English" --target "Vietnamese"`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("unpackedEpubPath is required. Please provide the path to the unpacked EPUB directory.")
		}

		return util.ValidateEpubPath(args[0])
	},
	RunE: runTranslate,
}

func init() {
	Translate.Flags().StringVar(&sourceLanguage, "source", "English", "source language")
	Translate.Flags().StringVar(&targetLanguage, "target", "Vietnamese", "target language")
	Translate.Flags().String("model", string(anthropic.ModelClaude3Dot5SonnetLatest), "Anthropic model to use")
}

type elementToTranslate struct {
	filePath      string
	contentEl     *goquery.Selection
	doc           *goquery.Document
	totalElements int
	index         int
	content       string
}

type translationBatch struct {
	elements []elementToTranslate
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

	// Extract book name from EPUB metadata
	bookName, err := extractBookName(unzipPath)
	if err != nil {
		return fmt.Errorf("error extracting book name: %v", err)
	}

	limiter := rate.NewLimiter(rate.Every(time.Minute/50), 10)

	anthropicTranslator, err := translator.GetAnthropicTranslator(&translator.Config{
		APIKey:      os.Getenv("ANTHROPIC_KEY"),
		Model:       cmd.Flag("model").Value.String(),
		Temperature: 0.7,
		MaxTokens:   8192,
	})
	if err != nil {
		return fmt.Errorf("error getting translator: %v", err)
	}

	// 1 worker and 1 job at a time, mean 1 file at a time
	err = processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      1,
		JobBuffer:    1,
		ResultBuffer: 10,
	}, func(ctx context.Context, filePath string) error {
		return processFileDirectly(ctx, filePath, anthropicTranslator, limiter, bookName)
	})

	return err
}

func processFileDirectly(ctx context.Context, filePath string, translator translator.Translator, limiter *rate.Limiter, bookName string) error {
    fmt.Printf("\nProcessing file: %s\n", path.Base(filePath))
    
    doc, err := openAndReadFile(filePath)
    if err != nil {
        return err
    }

    ensureUTF8Charset(doc)

    selector := fmt.Sprintf("[%s]:not([%s])", util.ContentIdKey, util.TranslationByIdKey)
    elements := doc.Find(selector)

    if elements.Length() == 0 {
        fmt.Printf("No elements to translate in %s\n", path.Base(filePath))
        return nil
    }

    fmt.Printf("Found %d elements to translate in %s\n", 
        elements.Length(), path.Base(filePath))

    // Create batches directly
    var currentBatch translationBatch
    maxBatchLength := 2000

    elements.Each(func(i int, contentEl *goquery.Selection) {
        select {
        case <-ctx.Done():
            return
        default:
            htmlContent, err := contentEl.Html()
            if err != nil || len(htmlContent) <= 1 {
                return
            }

            element := elementToTranslate{
                filePath:      filePath,
                contentEl:     contentEl,
                doc:           doc,
                totalElements: elements.Length(),
                index:         i,
                content:      htmlContent,
            }

            currentBatchLength := getBatchLength(&currentBatch)
            if currentBatchLength+len(htmlContent) > maxBatchLength && len(currentBatch.elements) > 0 {
                // Process current batch
                processBatch(ctx, filePath, currentBatch, translator, limiter, bookName)
                // Start new batch
                currentBatch = translationBatch{
                    elements: []elementToTranslate{element},
                }
            } else {
                currentBatch.elements = append(currentBatch.elements, element)
            }
        }
    })

    // Process final batch if not empty
    if len(currentBatch.elements) > 0 {
        processBatch(ctx, filePath, currentBatch, translator, limiter, bookName)
    }

    return nil
}

func extractBookName(unzipPath string) (string, error) {
    container, err := loader.ParseContainer(unzipPath)
    if err != nil {
        return "", fmt.Errorf("failed to parse container: %w", err)
    }

    packagePath := path.Join(unzipPath, container.Rootfile.FullPath)
    pkg, err := loader.ParsePackage(packagePath)
    if err != nil {
        return "", fmt.Errorf("failed to parse package: %w", err)
    }

    return pkg.Metadata.Title, nil
}

func getBatchLength(batch *translationBatch) int {
	var length int
	for _, element := range batch.elements {
		length += len(element.content)
	}
	return length
}

func processBatch(ctx context.Context, filePath string, batch translationBatch, anthropicTranslator translator.Translator, limiter *rate.Limiter, bookName string) {
	if len(batch.elements) == 0 {
		return
	}

	fmt.Printf("\nTranslating batch from file %s (segments: %d; length: %d)\n", 
		path.Base(filePath), len(batch.elements), getBatchLength(&batch))

	// Combine contents with more distinct markers and instructions
	var combinedContent strings.Builder
	combinedContent.WriteString("Translate the following HTML segments. Each segment is marked with BEGIN_SEGMENT_X and END_SEGMENT_X markers. Preserve these markers exactly in your response and maintain all HTML tags.\n\n")
	
	for i, element := range batch.elements {
		combinedContent.WriteString(fmt.Sprintf("<SEGMENT_%d>\n%s\n</SEGMENT_%d>\n\n", i, element.content, i))
	}

	// Translate combined content
	translatedContent, err := retryTranslate(ctx, anthropicTranslator, limiter, combinedContent.String(), sourceLanguage, targetLanguage, bookName)
	if err != nil {
		fmt.Printf("Batch translation error: %v\n", err)
		return
	}

	// Split translated content and process individual elements
	translations := splitTranslations(translatedContent)
	if len(translations) != len(batch.elements) {
		fmt.Printf("Translation segments mismatch for %s: got %d, expected %d\n", 
			path.Base(filePath), len(translations), len(batch.elements))
		return
	}

	fmt.Printf("Successfully translated batch from %s, writing to file...\n", path.Base(filePath))

	fileLock := getFileLock(filePath)
	fileLock.Lock()
	defer fileLock.Unlock()

	for i, element := range batch.elements {
		if isTranslationValid(element.content, translations[i]) {
			if err := manipulateHTML(element.contentEl, targetLanguage, translations[i]); err != nil {
				fmt.Printf("HTML manipulation error: %v\n", err)
				continue
			}
		}
	}

	if err := writeContentToFile(filePath, batch.elements[0].doc); err != nil {
		fmt.Printf("Error writing to file: %v\n", err)
	}
}

func splitTranslations(translatedContent string) []string {
	var translations []string
	segments := strings.Split(translatedContent, "<SEGMENT_")
	
	for i := 1; i < len(segments); i++ {
		if parts := strings.Split(segments[i], "</SEGMENT_"); len(parts) > 1 {
			// Extract segment number and content
			segmentContent := parts[0]
			if idx := strings.Index(segmentContent, ">"); idx != -1 {
				content := strings.TrimSpace(segmentContent[idx+1:])
				translations = append(translations, content)
			}
		}
	}
	
	return translations
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

func retryTranslate(ctx context.Context, t translator.Translator, limiter *rate.Limiter, content, sourceLang, targetLang, bookName string) (string, error) {
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

			translatedContent, err := t.Translate(ctx, "", content, sourceLang, targetLang, bookName)
			if err == nil {
				return translatedContent, nil
			}

			if errors.Is(err, translator.ErrRateLimitExceeded) {
				time.Sleep(calculateBackoff(attempt, baseDelay*10))
			} else {
				time.Sleep(calculateBackoff(attempt, baseDelay))
			}

			fmt.Println("Failed to translate, retrying...", err)
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
		return true
	}

	// Ensure all HTML tags are preserved
	originalTags := extractHTMLTags(original)
	translatedTags := extractHTMLTags(translated)
	
	if len(originalTags) != len(translatedTags) {
		return false
	}
	
	for i := range originalTags {
		if originalTags[i] != translatedTags[i] {
			return false
		}
	}

	return true
}

func extractHTMLTags(html string) []string {
	var tags []string
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return tags
	}
	
	doc.Find("*").Each(func(i int, s *goquery.Selection) {
		tags = append(tags, goquery.NodeName(s))
	})
	
	return tags
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

func countWords(text string) int {
	words := strings.Fields(text)
	return len(words)
}