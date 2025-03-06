package translator

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
			client: anthropic.NewClient(cfg.APIKey, anthropic.WithBetaVersion(anthropic.BetaPromptCaching20240731),),
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

// Replace the promptLib map with embedded content
//go:embed prompts/psychology.txt
var psychologyPrompt string

//go:embed prompts/technical.txt
var technicalPrompt string

var promptLib = map[string]string{
	"psychology": psychologyPrompt,
	"technical":  technicalPrompt,
}

func createTranslationSystem(source, target, guidelines, bookName, promptPreset string) string {
	if guidelines == "" {
		guidelines = promptLib[promptPreset]
	}
	return fmt.Sprintf(guidelines, source, target, bookName)
}

func (a *Anthropic) Translate(ctx context.Context, promptPreset, content, source, target, bookName string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Create log entry
	logEntry := struct {
		Timestamp time.Time              `json:"timestamp"`
		Request   anthropic.MessagesRequest `json:"request"`
		Response  *anthropic.MessagesResponse `json:"response"`
		Error     string                 `json:"error,omitempty"`
	}{
		Timestamp: time.Now(),
	}

	cacheKey := generateCacheKey(promptPreset+content, source, target)

	if promptPreset != "" {
		if cachedTranslation, found := a.cache.Get(cacheKey); found {
			return cachedTranslation.(string), nil
		}
	}

	systemMessages := []anthropic.MessageSystemPart{
		{
			Type: "text",
			Text: createTranslationSystem(source, target, a.config.TranslationGuidelines, bookName, promptPreset),
			CacheControl: &anthropic.MessageCacheControl{
				Type: anthropic.CacheControlTypeEphemeral,
			},
		},
	}
	req := anthropic.MessagesRequest{
		Model:       anthropic.Model(a.config.Model),
		MultiSystem: systemMessages,
		Messages:    []anthropic.Message{anthropic.NewUserTextMessage(content)},
		Temperature: &a.config.Temperature,
		MaxTokens:   a.config.MaxTokens,
	}
	logEntry.Request = req

	resp, err := a.createMessageWithRetry(ctx, req)
	if err != nil {
		logEntry.Error = err.Error()
		a.writeLog(logEntry)
		return "", fmt.Errorf("createMessageWithRetry: %w", err)
	}

	logEntry.Response = resp
	a.writeLog(logEntry)

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

type CountTokensResponse struct {
    InputTokens int `json:"input_tokens"`
}

type CountTokensRequest struct {
    Model    string    `json:"model"`
    Messages []anthropic.Message `json:"messages"`
}

func (a *Anthropic) CountTokens(ctx context.Context, content string) (float32, error) {
    reqBody := CountTokensRequest{
        Model: string(a.config.Model),
        Messages: []anthropic.Message{
            {
                Role:    "user",
                Content: []anthropic.MessageContent{
                    {
                        Type: "text",
                        Text: &content,
                    },
                },
            },
        },
    }

    jsonBody, err := json.Marshal(reqBody)
    if err != nil {
        return 0, fmt.Errorf("marshal request body: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages/count_tokens", bytes.NewBuffer(jsonBody))
    if err != nil {
        return 0, fmt.Errorf("create request: %w", err)
    }

    req.Header.Set("x-api-key", a.config.APIKey)
    req.Header.Set("anthropic-version", "2023-06-01")
    req.Header.Set("anthropic-beta", "token-counting-2024-11-01")
    req.Header.Set("content-type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return 0, fmt.Errorf("do request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return 0, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
    }

    var tokenResp CountTokensResponse
    if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
        return 0, fmt.Errorf("decode response: %w", err)
    }

    return float32(tokenResp.InputTokens), nil
}

func generateCacheKey(content, source, target string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s", content, source, target)))
	return hex.EncodeToString(hash[:])
}

func (a *Anthropic) writeLog(entry interface{}) {
	logFile := filepath.Join("unpackage", "translator.log")
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		fmt.Printf("Error creating log directory: %v\n", err)
		return
	}

	// Marshal the entry with indentation for readability
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling log entry: %v\n", err)
		return
	}
	data = append(data, '\n') // Add newline between entries

	// Open file in append mode
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Error opening log file: %v\n", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		fmt.Printf("Error writing to log file: %v\n", err)
	}
}
