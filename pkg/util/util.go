package util

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
)

func GetUnzipPath(zipPath string) string {
	return path.Dir(zipPath) + "/" + path.Base(zipPath)[:len(path.Base(zipPath))-len(filepath.Ext(zipPath))]
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
