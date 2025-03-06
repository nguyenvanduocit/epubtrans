package editor

import (
	"context"
)

type Editor interface {
	// GenerateGuidelines generates translation guidelines for a given source and target language
	GenerateGuidelines(ctx context.Context, source string, target string, bookName string) (string, error)
}