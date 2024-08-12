package cmd

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	"github.com/nguyenvanduocit/epubtrans/pkg/loader"
	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var Serve = &cobra.Command{
	Use:     "serve [unpackedEpubPath]",
	Short:   "serve the content of an unpacked EPUB as a web server",
	Example: "epubtrans serve path/to/unpacked/epub",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("unpackedEpubPath is required")
		}

		return util.ValidateEpubPath(args[0])
	},
	RunE: runServe,
}

func init() {
	// port flag
	Serve.Flags().StringP("port", "p", "3000", "port to serve the EPUB content")
}

var ToInjectContentTypes = []string{
	"text/html",
	"application/xhtml+xml",
	"application/xml",
	"text/plain",
}

func shouldInject(contentType string) bool {
	for _, ct := range ToInjectContentTypes {
		if strings.Contains(contentType, ct) {
			return true
		}
	}
	return false
}

type TranslateRequest struct {
	FilePath           string `json:"file_path"`
	TranslationID      string `json:"translation_id"`
	TranslationContent string `json:"translation_content"`
}

type NavPoint struct {
	XMLName   xml.Name   `xml:"navPoint"`
	ID        string     `xml:"id,attr"`
	PlayOrder string     `xml:"playOrder,attr"`
	NavLabel  NavLabel   `xml:"navLabel"`
	Content   Content    `xml:"content"`
	NavPoints []NavPoint `xml:"navPoint"`
}

type NavLabel struct {
	Text string `xml:"text"`
}

type Content struct {
	Src string `xml:"src,attr"`
}

type NCX struct {
	XMLName xml.Name `xml:"ncx"`
	NavMap  NavMap   `xml:"navMap"`
}

type NavMap struct {
	NavPoints []NavPoint `xml:"navPoint"`
}

func generateTOCHTML(navPoints []NavPoint, level int) string {
	if len(navPoints) == 0 {
		return ""
	}

	var html strings.Builder
	html.WriteString("<ul>")

	for _, np := range navPoints {
		html.WriteString(fmt.Sprintf("<li><a href=\"%s\">%s</a>", np.Content.Src, np.NavLabel.Text))
		if len(np.NavPoints) > 0 {
			html.WriteString(generateTOCHTML(np.NavPoints, level+1))
		}
		html.WriteString("</li>")
	}

	html.WriteString("</ul>")
	return html.String()
}

const (
	githubRawContent = "https://raw.githubusercontent.com"
	userRepo         = "nguyenvanduocit/epubtrans"
	branch           = "main"
)

func runServe(cmd *cobra.Command, args []string) error {
	unpackedEpubPath := args[0]

	// Check if the directory exists
	if _, err := os.Stat(unpackedEpubPath); os.IsNotExist(err) {
		return fmt.Errorf("the specified directory does not exist: %s", unpackedEpubPath)
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	var scriptToInject = []byte(`<script src="/assets/app.js"></script><link rel="stylesheet" href="/assets/app.css">`)

	// Proxy route for assets
	app.Get("/assets/:filename", func(c *fiber.Ctx) error {
		filename := c.Params("filename")
		url := fmt.Sprintf("%s/%s/%s/assets/%s", githubRawContent, userRepo, branch, filename)

		// Make request to GitHub
		resp, err := http.Get(url)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Error fetching file")
		}
		defer resp.Body.Close()

		// Set content type based on file extension
		if strings.HasSuffix(filename, ".css") {
			c.Set("Content-Type", "text/css")
		} else if strings.HasSuffix(filename, ".js") {
			c.Set("Content-Type", "application/javascript")
		}

		//send the body to the client
		body, _ := io.ReadAll(resp.Body)
		return c.Send(body)
	})

	container, err := loader.ParseContainer(unpackedEpubPath)
	if err != nil {
		return err
	}

	contentDirPath := path.Dir(path.Join(unpackedEpubPath, container.Rootfile.FullPath))

	app.Get("/toc.html", func(c *fiber.Ctx) error {
		opfPath := filepath.Join(unpackedEpubPath, container.Rootfile.FullPath)
		pkg, err := loader.ParsePackage(opfPath)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(fmt.Sprintf("Error parsing package: %v", err))
		}

		tocItem := pkg.Manifest.GetItemByID(pkg.Spine.Toc)

		if tocItem == nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Error getting toc item")
		}

		tocPath := path.Join(contentDirPath, tocItem.Href)
		// Read the toc.ncx file
		tocContent, err := ioutil.ReadFile(tocPath)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Error reading toc.ncx file: " + tocPath)
		}

		var ncx NCX
		err = xml.Unmarshal(tocContent, &ncx)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Error parsing toc.ncx file")
		}

		// Generate HTML TOC
		tocHTML := generateTOCHTML(ncx.NavMap.NavPoints, 0)

		// Wrap the TOC in a basic HTML structure
		fullHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Table of Contents</title>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; }
        ul { padding-left: 20px; }
    </style>
</head>
<body>
    <h1>Table of Contents</h1>
    %s
</body>
</html>
`, tocHTML)

		c.Set("Content-Type", "text/html")

		return c.SendString(fullHTML)
	})

	app.Static("/", contentDirPath, fiber.Static{
		Browse: true,
		ModifyResponse: func(c *fiber.Ctx) error {
			contentType := c.Response().Header.Peek("Content-Type")
			if !shouldInject(string(contentType)) {
				return nil
			}

			body := c.Response().Body()
			if len(body) == 0 {
				return nil
			}

			// Find the position of </body>
			pos := bytes.LastIndex(body, []byte("</body>"))
			if pos == -1 {
				return nil
			}

			// Create a new slice with the additional capacity
			newBody := make([]byte, len(body)+len(scriptToInject))

			// Copy the parts of the original body and insert the script
			copy(newBody, body[:pos])
			copy(newBody[pos:], scriptToInject)
			copy(newBody[pos+len(scriptToInject):], body[pos:])

			c.Response().SetBody(newBody)
			c.Response().Header.SetContentLength(len(newBody))
			return nil
		},
	})

	app.Patch("/api/translate", func(c *fiber.Ctx) error {
		var req TranslateRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "Invalid request"})
		}

		filePath := path.Join(contentDirPath, req.FilePath)
		// Read the file
		content, err := os.ReadFile(filePath)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to read file"})
		}

		// Parse the HTML
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(content)))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to parse HTML"})
		}

		// Find the element and update its content
		updated := false
		doc.Find("[data-translation-id]").Each(func(i int, s *goquery.Selection) {
			if id, exists := s.Attr("data-translation-id"); exists && id == req.TranslationID {
				s.SetHtml(req.TranslationContent)
				updated = true
			}
		})

		if !updated {
			return c.Status(404).JSON(fiber.Map{"error": "Translation ID not found"})
		}

		// Write the updated content back to the file
		html, err := doc.Html()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to generate HTML"})
		}

		err = os.WriteFile(filePath, []byte(html), 0644)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to write file"})
		}

		return c.JSON(fiber.Map{"message": "Translation updated successfully"})
	})

	// API endpoint to get ebook information
	app.Get("/api/info", func(c *fiber.Ctx) error {
		opfPath := filepath.Join(unpackedEpubPath, container.Rootfile.FullPath)
		pkg, err := loader.ParsePackage(opfPath)
		if err != nil {
			return c.Status(500).SendString(fmt.Sprintf("Error parsing package: %v", err))
		}

		return c.JSON(pkg.Metadata)
	})

	// API endpoint to get manifest items
	app.Get("/api/manifest", func(c *fiber.Ctx) error {
		opfPath := filepath.Join(unpackedEpubPath, container.Rootfile.FullPath)
		pkg, err := loader.ParsePackage(opfPath)
		if err != nil {
			return c.Status(500).SendString(fmt.Sprintf("Error parsing package: %v", err))
		}

		return c.JSON(pkg.Manifest)
	})

	// API endpoint to get spine items
	app.Get("/api/spine", func(c *fiber.Ctx) error {
		opfPath := filepath.Join(unpackedEpubPath, container.Rootfile.FullPath)
		pkg, err := loader.ParsePackage(opfPath)
		if err != nil {
			return c.Status(500).SendString(fmt.Sprintf("Error parsing package: %v", err))
		}

		return c.JSON(pkg.Spine)
	})

	port := cmd.Flag("port").Value.String()

	slog.Info("- http://localhost:" + port + "/api/info")
	slog.Info("- http://localhost:" + port + "/toc.html")
	slog.Info("- http://localhost:" + port + "/api/manifest")
	slog.Info("- http://localhost:" + port + "/api/spine")

	return app.Listen(net.JoinHostPort("", port))
}
