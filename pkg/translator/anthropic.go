package translator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/liushuangls/go-anthropic/v2"
)

var (
	_anthropic    *Anthropic
	anthropicOnce sync.Once
)

type Config struct {
	APIKey                string
	Model                 string
	Temperature           float32
	MaxTokens             int
	CacheTTL              time.Duration
	CacheMaxCost          int64
	TranslationGuidelines string // New field for translation guidelines
	SystemPrompt          string // New field for system prompt
}

type UsageMetadata struct {
	TotalCalls     int                       `json:"total_calls"`
	LastUsed       time.Time                 `json:"last_used"`
	ModelUsage     map[string]int            `json:"model_usage"`
	PromptExamples []string                  `json:"prompt_examples"`
	TokenUsage     atomic.Uint64             `json:"token_usage"`
	TokenUsageList []anthropic.MessagesUsage `json:"token_usage_list"`
}

func GetAnthropicTranslator(cfg *Config) (*Anthropic, error) {
	var err error
	anthropicOnce.Do(func() {
		if cfg == nil {
			cfg = &Config{
				APIKey:      os.Getenv("ANTHROPIC_KEY"),
				Model:       string(anthropic.ModelClaude3Dot5SonnetLatest),
				Temperature: 0.3,
				MaxTokens:   8192,
			}
		}

		if cfg.APIKey == "" {
			err = errors.New("missing ANTHROPIC_KEY")
			return
		}

		if cfg.TranslationGuidelines == "" {
			cfg.TranslationGuidelines = os.Getenv("TRANSLATION_GUIDELINES")
		}
		if cfg.SystemPrompt == "" {
			cfg.SystemPrompt = os.Getenv("SYSTEM_PROMPT")
		}

		cfg.CacheTTL = 15 * time.Minute
		cfg.CacheMaxCost = 1e7

		cache, cacheErr := ristretto.NewCache(&ristretto.Config{
			NumCounters: 1e7,              // number of keys to track frequency of (10M).
			MaxCost:     cfg.CacheMaxCost, // maximum cost of cache (1GB).
			BufferItems: 64,               // number of keys per Get buffer.
		})
		if cacheErr != nil {
			err = fmt.Errorf("failed to create cache: %w", cacheErr)
			return
		}

		_anthropic = &Anthropic{
			client: anthropic.NewClient(cfg.APIKey, anthropic.WithBetaVersion("prompt-caching-2024-07-31")),
			cache:  cache,
			config: cfg,
			metadata: &UsageMetadata{
				ModelUsage: make(map[string]int),
			},
		}

		_anthropic.loadMetadata(context.Background()) // Pass a background context
	})

	if err != nil {
		return nil, err
	}

	return _anthropic, nil
}

func (a *Anthropic) loadMetadata(ctx context.Context) {
	data, err := os.ReadFile(a.getMetadataFilePath())
	if err != nil {
		return // File doesn't exist or can't be read, use default values
	}

	err = json.Unmarshal(data, a.metadata)
	if err != nil {
		fmt.Printf("Error unmarshaling metadata: %v\n", err)
	}
}

func (a *Anthropic) saveMetadata(ctx context.Context) {
	data, err := json.MarshalIndent(a.metadata, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling metadata: %v\n", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(a.getMetadataFilePath()), 0755)
	if err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	err = os.WriteFile(a.getMetadataFilePath(), data, 0644)
	if err != nil {
		fmt.Printf("Error writing metadata file: %v\n", err)
	}
}

func (a *Anthropic) getMetadataFilePath() string {
	return filepath.Join("unpackage", "translator_metadata.json")
}

type Anthropic struct {
	client   *anthropic.Client
	cache    *ristretto.Cache
	config   *Config
	metadata *UsageMetadata
	mu       sync.Mutex
}

func createTranslationSystem(source, target, guidelines, bookName string) string {
	if guidelines == "" {
		guidelines = `You are a skilled translator who excels at making complex psychology concepts simple and accessible for everyday readers. Your task is to translate the psychology book "%[3]s" from %[1]s to %[2]s, focusing on creating an engaging and easy-to-understand version for general readers.

Translation guidelines:

1. Core principles:
   - Explain like you're talking to a friend
   - Use everyday language and examples
   - Break down complex ideas into simple terms
   - Focus on practical applications
   - Make it relatable to daily life
   - Do not give extra explanation for the title, section titles, or headings

2. Writing style:
   - Warm and conversational tone
   - Short, clear sentences
   - Simple words over technical terms

3. Making concepts accessible:
   - Replace technical terms with everyday words
   - Use real-life examples and situations
   - Connect ideas to common experiences
   - Add helpful metaphors and comparisons

4. Cultural relevance:
   - Use local examples and situations
   - Reference familiar cultural elements
   - Include relatable daily scenarios
   - Adapt examples to local context
   - Use local expressions when appropriate

5. Target audience:
   - People with no psychology background
   - Readers seeking self-help and personal growth
   - Anyone interested in understanding themselves better
   - People who prefer simple, practical advice
   - Readers who avoid academic or technical books

6. Making it practical:
   - Focus on how to apply concepts
   - Include everyday examples
   - Connect to common life situations

7. Language choices:
   - Choose words a 12-year-old could understand
   - Explain any necessary technical terms simply

8. Sử dụng các thuật ngữ tâm lý học phổ biến trong tiếng việt, ví dụ:

   - narcissism: ái kỷ

Translate as if you're explaining to a friend who's curious about psychology but has no background in it. Focus on making the content engaging, practical, and immediately useful in daily life. The translation should feel like reading an interesting conversation rather than a textbook.`
	}
	return fmt.Sprintf(guidelines, source, target, bookName)
}

func (a *Anthropic) Translate(ctx context.Context, prompt, content, source, target, bookName string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cacheKey := generateCacheKey(prompt+content, source, target)

	if prompt != "" {
		if cachedTranslation, found := a.cache.Get(cacheKey); found {
			return cachedTranslation.(string), nil
		}
	}

	systemMessages := []anthropic.MessageSystemPart{
		{
			Type: "text",
			Text: createTranslationSystem(source, target, a.config.TranslationGuidelines, bookName),
			CacheControl: &anthropic.MessageCacheControl{
				Type: anthropic.CacheControlTypeEphemeral,
			},
		},
	}

	if prompt != "" {
		systemMessages = append(systemMessages, anthropic.MessageSystemPart{
			Type: "text",
			Text: prompt,
		})
	}

	resp, err := a.createMessageWithRetry(ctx, anthropic.MessagesRequest{
		Model:       anthropic.Model(a.config.Model),
		MultiSystem: systemMessages,
		Messages:    []anthropic.Message{anthropic.NewUserTextMessage("Translate this and not say anything otherwise the translation: " + content)},
		Temperature: &a.config.Temperature,
		MaxTokens:   a.config.MaxTokens,
	})

	if err != nil {
		return "", fmt.Errorf("createMessageWithRetry: %w", err)
	}

	if len(resp.Content) == 0 {
		return "", errors.New("no translation received")
	}

	translation := resp.GetFirstContentText()
	a.cache.SetWithTTL(cacheKey, translation, 0, a.config.CacheTTL)

	// Update metadata
	a.metadata.TotalCalls++
	a.metadata.LastUsed = time.Now()
	a.metadata.ModelUsage[a.config.Model]++
	if len(a.metadata.PromptExamples) < 5 {
		a.metadata.PromptExamples = append(a.metadata.PromptExamples, content[:min(100, len(content))])
	}

	// Update token usage
	totalTokens := uint64(resp.Usage.InputTokens + resp.Usage.OutputTokens)
	a.metadata.TokenUsage.Add(totalTokens)
	a.metadata.TokenUsageList = append(a.metadata.TokenUsageList, resp.Usage)

	// Save updated metadata
	a.saveMetadata(ctx) // Pass the context to saveMetadata

	return translation, nil
}

const maxRetries = 3

func (a *Anthropic) createMessageWithRetry(ctx context.Context, req anthropic.MessagesRequest) (*anthropic.MessagesResponse, error) {
	var resp anthropic.MessagesResponse
	var err error

	for retries := 0; retries < maxRetries; retries++ {
		resp, err = a.client.CreateMessages(ctx, req)
		if err == nil {
			return &resp, nil
		}

		var apiErr *anthropic.APIError
		if errors.As(err, &apiErr) && apiErr.IsRateLimitErr() {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(retries+1) * time.Second):
				fmt.Println("\t\t\tretrying after rate limit error")
				continue
			}
		}

		return nil, err
	}

	return nil, fmt.Errorf("max retries reached: %w", err)
}

func generateCacheKey(content, source, target string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s", content, source, target)))
	return hex.EncodeToString(hash[:])
}
