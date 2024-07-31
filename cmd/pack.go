package cmd

import (
	"archive/zip"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
)

var Pack = &cobra.Command{
	Use:   "pack",
	Short: "Zip files in a directory",
	Long:  "Zip files in a directory and create a new zip file",
	Args:  cobra.ExactArgs(1),
	RunE:  runPack,
}

func runPack(cmd *cobra.Command, args []string) error {
	srcDir := args[0]
	return packFiles(srcDir)
}

func packFiles(srcDir string) error {
	repackPath := getUniqueFilename(srcDir + "-repack.epub")
	fmt.Printf("Creating zip file: %s\n", repackPath)

	newZipFile, err := os.Create(repackPath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer newZipFile.Close()

	zipWriter := zip.NewWriter(newZipFile)
	defer zipWriter.Close()

	fileCount := 0
	totalSize := int64(0)

	err = filepath.Walk(srcDir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking directory: %w", err)
		}

		if info.IsDir() {
			return nil // Skip directories
		}

		relPath, err := filepath.Rel(srcDir, filePath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		zipFileHeader, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("failed to create file header: %w", err)
		}
		zipFileHeader.Name = relPath
		zipFileHeader.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(zipFileHeader)
		if err != nil {
			return fmt.Errorf("failed to create zip entry: %w", err)
		}

		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()

		written, err := io.Copy(writer, file)
		if err != nil {
			return fmt.Errorf("failed to write file to zip: %w", err)
		}

		fileCount++
		totalSize += written
		fmt.Printf("Added file: %s (%.2f KB)\n", relPath, float64(written)/1024)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to pack files: %w", err)
	}

	fmt.Printf("\nZip creation complete:\n")
	fmt.Printf("Total files: %d\n", fileCount)
	fmt.Printf("Total size: %.2f MB\n", float64(totalSize)/(1024*1024))
	fmt.Printf("Output file: %s\n", repackPath)

	return nil
}

func getUniqueFilename(filename string) string {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return filename
	}

	ext := filepath.Ext(filename)
	name := filename[:len(filename)-len(ext)]
	counter := 1

	for {
		newName := fmt.Sprintf("%s-(%d)%s", name, counter, ext)
		if _, err := os.Stat(newName); os.IsNotExist(err) {
			return newName
		}
		counter++
	}
}
