package editor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"google.golang.org/genai"
)

var genAiClient = sync.OnceValue[*genai.Client](func() *genai.Client {
	apiKey := os.Getenv("GOOGLE_AI_API_KEY")
	if apiKey == "" {
		panic("GOOGLE_AI_API_KEY environment variable must be set")
	}

	cfg := &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	}

	client, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to create Gemini client: %s", err))
	}

	return client
})

type Gemini struct {
	client *genai.Client
}

func NewGemini() *Gemini {
	return &Gemini{client: genAiClient()}
}

func (g *Gemini) GenerateGuidelines(ctx context.Context, source string, target string, bookName string) (string, error) {
	resp, err := genAiClient().Models.GenerateContent(context.Background(),
		"gemini-2.0-flash-thinking-exp-01-21",
		[]*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{
						Text: "Analyze the book title: \"" + bookName + "\" and generate appropriate guidelines for translating from " + source + " to " + target + ".",
					},
				},
			},
		},
		&genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Role: "system",
				Parts: []*genai.Part{
					{
						Text: "You are a translation guidelines generator. Create concise section outlines for translating this book. wrap the guilde line onto codeblock ```guidelines```. Follow this structure:\n\n" +
							"### BOOK ANALYSIS\n" +
							"- What's the genre and target audience?\n" +
							"- What special content considerations apply?\n\n" +
							"### TERMINOLOGY\n" +
							"- Which terms should remain untranslated?\n" +
							"- How should technical terms be handled?\n\n" +
							"### WRITING STYLE\n" +
							"- What tone and formality level is needed?\n" +
							"- How should cultural context be adapted?\n\n" +
							"### ACCURACY\n" +
							"- What are the key accuracy priorities?\n" +
							"- What context must be preserved?\n\n" +
							"### FORMATTING\n" +
							"- How should HTML/markup be handled?\n" +
							"- What formatting must be preserved?\n\n" +
							"### QUALITY CHECKS\n" +
							"- What are the key verification points?\n" +
							"- What's the final quality checklist?\n\n" +
							"Provide brief, actionable guidance for each point.",
					},
				},
			},
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to generate guidelines: %w", err)
	}

	text, err := resp.Text()
	if err != nil {
		return "", fmt.Errorf("failed to get response text: %w", err)
	}

	// Find the content between ```guidelines and ```
	startMarker := "```guidelines"
	endMarker := "```"
	
	startIndex := strings.Index(text, startMarker)
	if startIndex == -1 {
		return "", fmt.Errorf("guidelines block start marker not found in response")
	}
	startIndex += len(startMarker)
	
	endIndex := strings.LastIndex(text, endMarker)
	if endIndex == -1 {
		return "", fmt.Errorf("guidelines block end marker not found in response")
	}
	
	// Extract and trim the content between the markers
	guidelines := strings.TrimSpace(text[startIndex:endIndex])
	
	return guidelines, nil
}
