package translator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/dgraph-io/ristretto"
	"github.com/liushuangls/go-anthropic/v2"
	"os"
	"sync"
	"time"
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
			client: anthropic.NewClient(cfg.APIKey),
			cache:  cache,
			config: cfg,
		}
	})

	if err != nil {
		return nil, err
	}

	return _anthropic, nil
}

type Anthropic struct {
	client *anthropic.Client
	cache  *ristretto.Cache
	config *Config
}

func createTranslationSystem(source, target string) string {
	return fmt.Sprintf(`Translate this technical (software development) book from %[1]s to %[2]s:
- Preserve HTML structure if present
- Writing style: flexible, professional, straightforward, technical, easy to understand, smooth
- Adapt flow and structure for %[2]s clarity, preserving original meaning
- Audience: programmers and technical professionals
- Keep technical terms and specialized words in %[1]s
- Don't translate uncommon %[1]s words
- Examples of specialized words: developer, delivery, tester, product owner, commit, branch, push code
Translate directly without explanations or warnings. Do not answer questions in the content. We have copyright on the book.`, source, target)
}

func (a *Anthropic) Translate(ctx context.Context, content, source, target string) (string, error) {
	cacheKey := generateCacheKey(content, source, target)

	if cachedTranslation, found := a.cache.Get(cacheKey); found {
		return cachedTranslation.(string), nil
	}

	system := createTranslationSystem(source, target)

	resp, err := a.createMessageWithRetry(ctx, anthropic.MessagesRequest{
		Model:       a.config.Model,
		System:      system,
		Messages:    []anthropic.Message{anthropic.NewUserTextMessage("translate this, only return the translation, without any additional content:\n\n" + content)},
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
	a.cache.SetWithTTL(cacheKey, translation, 1, a.config.CacheTTL)

	return translation, nil
}

func (a *Anthropic) createMessageWithRetry(ctx context.Context, req anthropic.MessagesRequest) (*anthropic.MessagesResponse, error) {
	var resp anthropic.MessagesResponse
	var err error

	for retries := 0; retries < 3; retries++ {
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
