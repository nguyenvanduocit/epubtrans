package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func GetUnzipDestination(zipPath string) (string, error) {
	if zipPath == "" {
		return "", errors.New("zipPath cannot be empty")
	}

	// Clean the path to handle any ".." or "." components
	zipPath = filepath.Clean(zipPath)

	// Get the directory and filename
	dir := filepath.Dir(zipPath)
	filename := filepath.Base(zipPath)

	// Remove the extension (if any)
	extIndex := strings.LastIndex(filename, ".")
	if extIndex > 0 {
		filename = filename[:extIndex]
	}

	// Join the directory and filename to create the destination path
	result := filepath.Join(dir, filename)

	// Handle the root directory case
	if dir == "/" && !strings.HasPrefix(result, "/") {
		result = "/" + result
	}

	return result, nil
}

func ValidateEpubPath(epubPath string) error {
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

func IsEmptyOrWhitespace(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

func IsNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
