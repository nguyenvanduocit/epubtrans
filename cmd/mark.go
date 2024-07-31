package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/net/html"

	"epubtrans/pkg/loader"
	"epubtrans/pkg/util"
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
	Use:   "mark [epub_path]",
	Short: "Mark content in EPUB files",
	Args:  cobra.ExactArgs(1),
	RunE:  markContent,
}

func init() {
	Mark.Flags().Int("workers", runtime.NumCPU(), "Number of worker goroutines")
}

func markContent(cmd *cobra.Command, args []string) error {
	epubPath := args[0]

	if err := validateEPUBPath(epubPath); err != nil {
		return err
	}

	container, err := loader.ParseContainer(epubPath)
	if err != nil {
		return fmt.Errorf("parsing container: %w", err)
	}

	containerFileAbsPath := filepath.Join(epubPath, container.Rootfile.FullPath)
	pkg, err := loader.ParsePackage(containerFileAbsPath)
	if err != nil {
		return fmt.Errorf("parsing package: %w", err)
	}

	contentDir := filepath.Dir(containerFileAbsPath)
	return processPackageItems(pkg, contentDir)
}

func validateEPUBPath(epubPath string) error {
	fi, err := os.Stat(epubPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("epub path %s does not exist", epubPath)
		}
		return fmt.Errorf("checking epub path: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("epub path %s is not a directory", epubPath)
	}
	return nil
}

func processPackageItems(pkg *loader.Package, contentDir string) error {
	jobs := make(chan string, len(pkg.Manifest.Items))
	results := make(chan error, len(pkg.Manifest.Items))

	workerCount := 5
	var wg sync.WaitGroup
	for w := 1; w <= workerCount; w++ {
		wg.Add(1)
		go worker(jobs, results, &wg)
	}

	for _, item := range pkg.Manifest.Items {
		if item.MediaType != "application/xhtml+xml" {
			continue
		}
		filePath := filepath.Join(contentDir, item.Href)
		jobs <- filePath
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	for err := range results {
		if err != nil {
			fmt.Printf("Error processing file: %v\n", err)
		}
	}

	return nil
}

func worker(jobs <-chan string, results chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	for filePath := range jobs {
		err := markContentInFile(filePath)
		results <- err
	}
}

func markContentInFile(filePath string) error {
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
