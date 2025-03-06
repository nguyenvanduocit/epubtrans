package util

import (
	"os"

	"github.com/PuerkitoBio/goquery"
)

func OpenAndReadFile(filePath string) (*goquery.Document, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return goquery.NewDocumentFromReader(file)
}