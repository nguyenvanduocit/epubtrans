package util

import (
	"path"
	"path/filepath"
)

func GetUnzipPath(zipPath string) string {
	return path.Dir(zipPath) + "/" + path.Base(zipPath)[:len(path.Base(zipPath))-len(filepath.Ext(zipPath))]
}
