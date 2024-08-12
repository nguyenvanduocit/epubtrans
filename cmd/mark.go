package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/nguyenvanduocit/epubtrans/pkg/processor"
	"github.com/spf13/cobra"
	"golang.org/x/net/html"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/nguyenvanduocit/epubtrans/pkg/util"
)

var blacklist = []string{
	"math",
	"figure",
	"pre",
	"code",
	"head",
	"script",
	"style",
	"template",
}

// Mark represents the command for marking content in EPUB files
var Mark = &cobra.Command{
	Use:     "mark [epub_path]",
	Short:   "Mark content in EPUB files",
	Long:    "Mark content in EPUB files by adding a unique ID to each content node",
	Example: "epubtrans mark path/to/unpacked/epub",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("unpackedEpubPath is required")
		}

		return util.ValidateEpubPath(args[0])
	},
	RunE: runMark,
}

func init() {
	Mark.Flags().Int("workers", runtime.NumCPU(), "Number of worker goroutines")
}

func runMark(cmd *cobra.Command, args []string) error {
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

	workers, _ := cmd.Flags().GetInt("workers")

	return processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      workers,
		JobBuffer:    10,
		ResultBuffer: 10,
	}, markContentInFile)
}

func markContentInFile(ctx context.Context, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		return fmt.Errorf("parsing HTML in file %s: %w", filePath, err)
	}

	processNode(doc)

	f, err = os.Create(filePath)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", filePath, err)
	}
	defer f.Close()

	if err := html.Render(f, doc); err != nil {
		return fmt.Errorf("rendering HTML to file %s: %w", filePath, err)
	}

	return nil
}
func processNode(n *html.Node) {
	if n.Type == html.ElementNode {
		// Skip if already marked
		for _, attr := range n.Attr {
			if attr.Key == util.ContentIdKey {
				return
			}
		}

		// Skip if blacklisted
		for _, tag := range blacklist {
			if n.Data == tag {
				return
			}
		}

		if !isContainer(n) {
			content := extractTextContent(n)
			if content != "" {
				// Mark this node
				randomID, err := generateContentID([]byte(content))
				if err != nil {
					fmt.Printf("Error generating content ID: %v\n", err)
					return
				}
				n.Attr = append(n.Attr, html.Attribute{Key: util.ContentIdKey, Val: randomID})
				return
			}
		}
	}

	// Process child nodes
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		processNode(c)
	}
}

func isContainer(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}

	hasElementChild := false
	hasTextContent := false

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			hasElementChild = true
		} else if c.Type == html.TextNode && strings.TrimSpace(c.Data) != "" {
			hasTextContent = true
		}
	}

	return hasElementChild && !hasTextContent
}

func extractTextContent(n *html.Node) string {
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			text += c.Data
		} else if c.Type == html.ElementNode {
			text += extractTextContent(c)
		}
	}
	return strings.TrimSpace(text)
}

func generateContentID(content []byte) (string, error) {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:]), nil
}
