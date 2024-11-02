package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/nguyenvanduocit/epubtrans/pkg/processor"
	"github.com/spf13/cobra"
	"golang.org/x/net/html"

	"github.com/nguyenvanduocit/epubtrans/pkg/util"
)

var blacklist = map[string]bool{
    "math":     true,
    "figure":   true,
    "pre":      true,
    "code":     true,
    "head":     true,
    "script":   true,
    "style":    true,
    "template": true,
    "svg":      true,
    "noscript": true,
}

var Mark = &cobra.Command{
	Use:     "mark [epub_path]",
	Short:   "Add unique identifiers to content nodes in EPUB files",
	Long:    "This command marks content in EPUB files by adding a unique ID to each content node, facilitating easier reference and manipulation of the content.",
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

	workers, err := cmd.Flags().GetInt("workers")
	if err != nil {
		return fmt.Errorf("getting workers flag: %w", err)
	}

	if workers <= 0 {
		return fmt.Errorf("workers must be greater than 0")
	}

	return processor.ProcessEpub(ctx, unzipPath, processor.Config{
		Workers:      workers,
		JobBuffer:    10,
		ResultBuffer: 10,
	}, markContentInFile)
}

func markContentInFile(ctx context.Context, filePath string) error {
	if filePath == "" {
		return fmt.Errorf("filePath cannot be empty")
	}

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

const minContentLength = 2

func processNode(n *html.Node) {
	if n.Type == html.ElementNode {
		// Skip if already marked
		for _, attr := range n.Attr {
			if attr.Key == util.ContentIdKey {
				return
			}
		}

		// Skip if blacklisted
		if blacklist[n.Data] {
			return
		}

		if !isContainer(n) {
			content := extractTextContent(n)
			if util.IsEmptyOrWhitespace(content) || len(content) <= minContentLength || util.IsNumeric(content) || isSpecialContent(content) {
				fmt.Printf("Skipping content in <%s> tag: %q\n", n.Data, content)
				return
			} else {
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

var re = regexp.MustCompile(`^[*=\-_.,:;!?#\s]+$`)

func isSpecialContent(content string) bool {
	return re.MatchString(content)
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
