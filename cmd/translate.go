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
	"github.com/nguyenvanduocit/epubtrans/pkg/editor"
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
	Translate.Flags().String("model", "claude-3-5-sonnet-20241022", "Anthropic model to use")
	Translate.Flags().String("prompt", "technical", "Prompt preset to use")
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
	wordCount float32
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

func saveGuidelines(unzipPath string, guidelines string) error {
	metaInfPath := path.Join(unzipPath, "META-INF")
	if err := os.MkdirAll(metaInfPath, 0755); err != nil {
		return fmt.Errorf("failed to create META-INF directory: %w", err)
	}

	guidelinesPath := path.Join(metaInfPath, "guidelines.txt")
	if err := os.WriteFile(guidelinesPath, []byte(guidelines), 0644); err != nil {
		return fmt.Errorf("failed to write guidelines file: %w", err)
	}

	return nil
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

	// Kiểm tra model flag
	model := cmd.Flag("model").Value.String()
	if model == "" {
		return fmt.Errorf("model flag is required")
	}

	// Kiểm tra API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	limiter := rate.NewLimiter(rate.Every(time.Minute/50), 10)

	// Check for existing guidelines
	guidelinesPath := path.Join(unzipPath, "META-INF", "guidelines.txt")
	guidelines := ""
	
	if guidelinesContent, err := os.ReadFile(guidelinesPath); err == nil {
		// Use existing guidelines
		guidelines = string(guidelinesContent)
		fmt.Println("Using existing translation guidelines from META-INF/guidelines.txt")
	} else {
		// Generate new guidelines if file doesn't exist
		geminiEditor := editor.NewGemini()
		newGuidelines, err := geminiEditor.GenerateGuidelines(ctx, sourceLanguage, targetLanguage, bookName)
		if err != nil {
			fmt.Printf("Warning: Failed to generate guidelines: %v\n", err)
		} else {
			guidelines = newGuidelines
			// Save guidelines to META-INF/guidelines.txt
			if err := saveGuidelines(unzipPath, guidelines); err != nil {
				fmt.Printf("Warning: Failed to save guidelines: %v\n", err)
			} else {
				fmt.Println("New translation guidelines saved to META-INF/guidelines.txt")
			}
		}
	}

	deepseekTranslator, err := translator.GetAnthropicTranslator(&translator.Config{
		APIKey:                apiKey,
		Model:                model,
		Temperature:          0.7,
		MaxTokens:           8192,
		TranslationGuidelines: guidelines,
	})
	if err != nil {
		return fmt.Errorf("error getting translator: %v", err)
	}

	promptPreset := cmd.Flag("prompt").Value.String()
	if promptPreset == "" {
		return fmt.Errorf("prompt flag is required")
	}

	// 1 worker and 1 job at a time, mean 1 file at a time
	err = processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      1,
		JobBuffer:    1,
		ResultBuffer: 10,
	}, func(ctx context.Context, filePath string) error {
		return processFileDirectly(ctx, filePath, deepseekTranslator, limiter, bookName, promptPreset)
	})

	return err
}

var estimatedTokensPerWord float32 = 1.5

func processFileDirectly(ctx context.Context, filePath string, translator translator.Translator, limiter *rate.Limiter, bookName string, promptPreset string) error {
	if translator == nil {
		return fmt.Errorf("translator is nil")
	}
	if limiter == nil {
		return fmt.Errorf("rate limiter is nil")
	}
	if bookName == "" {
		return fmt.Errorf("book name is empty")
	}
	if promptPreset == "" {
		return fmt.Errorf("prompt preset is empty")
	}

	fmt.Printf("\nProcessing file: %s\n", path.Base(filePath))
    
	doc, err := util.OpenAndReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to open and read file: %w", err)
	}

	ensureUTF8Charset(doc)

	selector := fmt.Sprintf("[%s]:not([%s])", util.ContentIdKey, util.TranslationByIdKey)
	elements := doc.Find(selector)

	if elements == nil {
		return fmt.Errorf("failed to find elements with selector: %s", selector)
	}

	if elements.Length() == 0 {
		fmt.Printf("No elements to translate in %s\n", path.Base(filePath))
		return nil
	}

	fmt.Printf("Found %d elements to translate in %s\n", 
		elements.Length(), path.Base(filePath))

	// Create batches directly
	currentBatch := translationBatch{
		wordCount: 0,
	}

	maxBatchLength := float32(1500)

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

			estimatedTokens := (currentBatch.wordCount + float32(len(strings.Fields(htmlContent)))) * estimatedTokensPerWord

			if estimatedTokens > maxBatchLength {	
				estimatedTokens = getBatchLength(ctx, &currentBatch, translator)
				fmt.Printf("Counted tokens: %f\n", estimatedTokens)
				estimatedTokensPerWord = estimatedTokens / currentBatch.wordCount // update estimated tokens per word
			} else {
				fmt.Printf("Estimated tokens: %f\n", estimatedTokens)
			}

			if estimatedTokens > maxBatchLength && len(currentBatch.elements) > 0 {
				// Process current batch
				processBatch(ctx, filePath, currentBatch, translator, limiter, bookName, promptPreset)
				// Start new batch
				currentBatch = translationBatch{
					elements: []elementToTranslate{element},
					wordCount: float32(len(strings.Fields(htmlContent))),
				}
			} else {
				currentBatch.elements = append(currentBatch.elements, element)
				currentBatch.wordCount += float32(len(strings.Fields(htmlContent)))
			}
		}
	})

	// Process final batch if not empty
	if len(currentBatch.elements) > 0 {
		processBatch(ctx, filePath, currentBatch, translator, limiter, bookName, promptPreset)
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

func getBatchLength(ctx context.Context, batch *translationBatch, translator translator.Translator) float32 {
	if batch == nil {
		fmt.Printf("Error: batch is nil\n")
		return 0
	}
	if translator == nil {
		fmt.Printf("Error: translator is nil\n")
		return 0
	}

	var allContent string
	for _, element := range batch.elements {
		if element.content == "" {
			continue
		}
		allContent += element.content
	}

	if allContent == "" {
		fmt.Printf("Warning: empty content in batch\n")
		return 0
	}
	
	count, err := translator.CountTokens(ctx, allContent)
	if err != nil {
		fmt.Printf("Error counting tokens: %v\n", err)
		return 0
	}
	
	return count
}

func processBatch(ctx context.Context, filePath string, batch translationBatch, anthropicTranslator translator.Translator, limiter *rate.Limiter, bookName string, promptPreset string) {
	if len(batch.elements) == 0 {
		return
	}

	fmt.Printf("\nTranslating batch from file %s (Elements: %d, Word Count: %f\n", 
		path.Base(filePath), len(batch.elements), batch.wordCount)

	// Combine contents with more distinct markers and instructions
	var combinedContent strings.Builder
	combinedContent.WriteString("Translate the following HTML segments. Each segment is marked with BEGIN_SEGMENT_X and END_SEGMENT_X markers. Preserve these markers exactly in your response and maintain all HTML tags.\n\n")
	
	for i, element := range batch.elements {
		combinedContent.WriteString(fmt.Sprintf("<SEGMENT_%d>\n%s\n</SEGMENT_%d>\n\n", i, element.content, i))
	}

	// Translate combined content
	translatedContent, err := retryTranslate(ctx, anthropicTranslator, limiter, combinedContent.String(), sourceLanguage, targetLanguage, bookName, promptPreset)
	if err != nil {
		fmt.Printf("Batch translation error: %v\n", err)
		return
	}

	// Split translated content and process individual elements
	translations := splitTranslations(translatedContent)
	if len(translations) != len(batch.elements) {
		fmt.Printf("Translation segments mismatch for %s: got %d, expected %d\n", 
			path.Base(filePath), len(translations), len(batch.elements))
		
		// Write debug information to file
		debugFilePath := filePath + ".debug.txt"
		debugContent := fmt.Sprintf("Original Request:\n%s\n\nTranslated Response:\n%s\n\nExpected segments: %d\nReceived segments: %d",
			combinedContent.String(),
			translatedContent,
			len(batch.elements),
			len(translations))
			
		if err := os.WriteFile(debugFilePath, []byte(debugContent), 0644); err != nil {
			fmt.Printf("Failed to write debug file: %v\n", err)
		} else {
			fmt.Printf("Debug information written to: %s\n", debugFilePath)
		}
		
		// Process as many translations as we have
		minLen := min(len(translations), len(batch.elements))
		translations = translations[:minLen]
		batch.elements = batch.elements[:minLen]
		
		fmt.Printf("Proceeding with %d valid translations\n", minLen)
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

func ensureUTF8Charset(doc *goquery.Document) {
	charset, _ := doc.Find("meta[charset]").Attr("charset")
	if charset != "utf-8" {
		doc.Find("head").AppendHtml(`<meta charset="utf-8">`)
	}
}

func retryTranslate(ctx context.Context, t translator.Translator, limiter *rate.Limiter, content, sourceLang, targetLang, bookName, promptPreset string) (string, error) {
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

			translatedContent, err := t.Translate(ctx, promptPreset, content, sourceLang, targetLang, bookName)
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

	// Check if translation is suspiciously long or short
	originalWords := countWords(original)
	translatedWords := countWords(translated)
	if translatedWords > originalWords*5 || translatedWords < originalWords/5 {
		return false
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}