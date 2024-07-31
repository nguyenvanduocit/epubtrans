package translator

import (
	"context"
	"errors"
)

var ErrRateLimitExceeded = errors.New("rate limit exceeded")

type Translator interface {
	Translate(ctx context.Context, content string, source string, target string) (string, error)
}
