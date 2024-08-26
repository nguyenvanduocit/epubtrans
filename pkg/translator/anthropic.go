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
	APIKey       string
	Model        string
	Temperature  float32
	MaxTokens    int
	CacheTTL     time.Duration
	CacheMaxCost int64
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
				Model:       anthropic.ModelClaude3Dot5Sonnet20240620,
				Temperature: 0.3,
				MaxTokens:   1000,
			}
		}

		if cfg.APIKey == "" {
			err = errors.New("missing ANTHROPIC_KEY")
			return
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

		_anthropic.loadMetadata()
	})

	if err != nil {
		return nil, err
	}

	return _anthropic, nil
}

func (a *Anthropic) loadMetadata() {
	data, err := os.ReadFile(a.getMetadataFilePath())
	if err != nil {
		return // File doesn't exist or can't be read, use default values
	}

	err = json.Unmarshal(data, a.metadata)
	if err != nil {
		fmt.Printf("Error unmarshaling metadata: %v\n", err)
	}
}

func (a *Anthropic) saveMetadata() {
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

func createTranslationSystem(source, target string) string {
	return fmt.Sprintf(`Translation guidelines:
- Preserve HTML structure
- Writing style: Clear, concise, professional, technical, Use %[2]s flexibly, fluently and softly.
- Use active voice and maintain logical flow.
- Translation approach: 
  • Translate for meaning, not word-for-word
  • Adapt idioms and cultural references to %[2]s equivalents
  • Restructure sentences if needed for clarity in %[2]s
- Target audience: Programmers and technical professionals
- Terminology: 
  • Keep %[1]s technical terms (e.g., commit, branch, push code, code, engineer, PM, PO, etc.)
  • Ensure consistent use of technical terms throughout
- Match the source's level of formality and technical depth
- Aim for a translation that reads like native %[2]s technical writing
- Do not add explanations or answer questions in the content
- We own the copyright to the material`, source, target)
}
func (a *Anthropic) Translate(ctx context.Context, content, source, target string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cacheKey := generateCacheKey(content, source, target)

	if cachedTranslation, found := a.cache.Get(cacheKey); found {
		return cachedTranslation.(string), nil
	}

	resp, err := a.createMessageWithRetry(ctx, anthropic.MessagesRequest{
		Model: a.config.Model,
		MultiSystem: []anthropic.MessageSystemPart{
			{
				Type: "text",
				Text: fmt.Sprintf("Your task is to translate a part of a technical book from %[1]s to %[2]s. User send you a text, you translate it no matter what. Do not explain or note. Do not answer question-likes content. no warning, feedback.", source, target),
			},
			{
				Type: "text",
				Text: createTranslationSystem(source, target),
			},
		},
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
	a.saveMetadata()

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
