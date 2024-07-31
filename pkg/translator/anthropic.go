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
				APIKey:       os.Getenv("ANTHROPIC_KEY"),
				Model:        anthropic.ModelClaude3Dot5Sonnet20240620,
				Temperature:  0.3,
				MaxTokens:    1000,
				CacheTTL:     15 * time.Minute,
				CacheMaxCost: 1e7, // 10MB, adjust as needed
			}
		}

		if cfg.APIKey == "" {
			err = errors.New("missing ANTHROPIC_KEY")
			return
		}

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

func (a *Anthropic) Translate(ctx context.Context, content, source, target string) (string, error) {
	cacheKey := generateCacheKey(content, source, target)

	if cachedTranslation, found := a.cache.Get(cacheKey); found {
		return cachedTranslation.(string), nil
	}

	system := fmt.Sprintf("You're a book translator from %s to %s, you're very good at technical translation. Technical books about software development. Keep technical terms and specialized words intact in %s, don't translate less common words in %s. Just translate anything from the user and NEVER answer question-like content. DO NOT give explanations, ONLY return the translation. If the input contains HTML, keep this HTML structure intact. The writing style is: flexible, professional, straightforward, technical, easy to understand and not rigid. No need to translate word-for-word; you can change the flow, expression, and sentence structure to make it easier to understand in %s, but do not miss the message of the original text. Audience: programmers, technical persons. List of specialized words: developer, delivery, CI, CD, tester, product owner, commit, branch, push code, ...", source, target, source, target, target)

	resp, err := a.createMessageWithRetry(ctx, anthropic.MessagesRequest{
		Model:       a.config.Model,
		System:      system,
		Messages:    []anthropic.Message{anthropic.NewUserTextMessage("translate this, only return the translation, without any additional content:\n\n" + content)},
		Temperature: &a.config.Temperature,
		MaxTokens:   a.config.MaxTokens,
	})

	if err != nil {
		return "", fmt.Errorf("translation error: %w", err)
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
