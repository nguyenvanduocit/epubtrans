package translator

import (
	"context"
	"errors"
)

var ErrRateLimitExceeded = errors.New("rate limit exceeded")

type Translator interface {
	Translate(ctx context.Context, promptPreset string, content string, source string, target string, bookName string) (string, error)
	CountTokens(ctx context.Context, content string) (float32, error)
}
