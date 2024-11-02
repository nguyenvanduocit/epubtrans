package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
)

const (
	defaultBufferSize = 32 * 1024 // 32KB
	channelBufferSize = 100
	defaultSuffix     = "-bilangual.epub"
)

var Pack = &cobra.Command{
	Use:   "pack [unpackedEpubPath]",
	Short: "Create an EPUB file from an unpacked directory",
	Long: `Pack creates a new EPUB file from an unpacked directory structure.
It compresses the contents and maintains the EPUB file structure.
This command is useful after modifying the contents of an unpacked EPUB.`,
	Example: "epubtrans pack /path/to/unpacked/epub",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("unpackedEpubPath is required")
		}

		return util.ValidateEpubPath(args[0])
	},
	RunE: runPack,
}

func init() {
	Pack.Flags().StringP("output", "o", "", "output file path")
}

func runPack(cmd *cobra.Command, args []string) error {
	srcDir := args[0]
	outputPath, _ := cmd.Flags().GetString("output")
	return packFiles(srcDir, outputPath)
}

func packFiles(srcDir string, outputPath string) error {
	if outputPath == "" {
		outputPath = getUniqueFilename(srcDir + defaultSuffix)
	} else {
		outputPath = getUniqueFilename(outputPath)
	}

	// Validate source directory
	if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
		return fmt.Errorf("invalid source directory: %w", err)
	}

	progress := &packingProgress{}

	fmt.Printf("Creating zip file: %s\n", outputPath)

	newZipFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer newZipFile.Close()

	zipWriter := zip.NewWriter(newZipFile)
	defer zipWriter.Close()

	// Create a buffered channel for file info
	fileInfoChan := make(chan fileInfo, channelBufferSize)

	// Start a single goroutine to write to the zip file
	var wg sync.WaitGroup
	wg.Add(1)
	var writeErr error
	go func() {
		defer wg.Done()
		for fi := range fileInfoChan {
			if err := validateFile(fi.path, fi.info); err != nil {
				writeErr = err
				return
			}

			if err := addFileToZip(zipWriter, fi, progress); err != nil {
				writeErr = err
				return
			}

			fmt.Printf("Added file: %s (%.2f KB)\n", fi.relPath, float64(fi.info.Size())/1024)
		}
	}()

	// Walk the directory and send file info to the channel
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

		fileInfoChan <- fileInfo{path: filePath, relPath: relPath, info: info}
		return nil
	})

	close(fileInfoChan)
	wg.Wait()

	if writeErr != nil {
		return fmt.Errorf("failed to write to zip: %w", writeErr)
	}

	if err != nil {
		return fmt.Errorf("failed to pack files: %w", err)
	}

	fmt.Printf("\nZip creation complete:\n")
	fmt.Printf("Total files: %d\n", progress.fileCount)
	fmt.Printf("Total size: %.2f MB\n", float64(progress.totalSize)/(1024*1024))
	fmt.Printf("Output file: %s\n", outputPath)

	return nil
}

type fileInfo struct {
	path    string
	relPath string
	info    os.FileInfo
}

type packingProgress struct {
	fileCount int64
	totalSize int64
	mu        sync.Mutex
}

func (p *packingProgress) update(size int64) {
	atomic.AddInt64(&p.fileCount, 1)
	atomic.AddInt64(&p.totalSize, size)
}

func addFileToZip(zipWriter *zip.Writer, fi fileInfo, progress *packingProgress) error {
	zipFileHeader, err := zip.FileInfoHeader(fi.info)
	if err != nil {
		return fmt.Errorf("failed to create file header: %w", err)
	}
	zipFileHeader.Name = fi.relPath
	zipFileHeader.Method = chooseCompressionMethod(fi.path)

	writer, err := zipWriter.CreateHeader(zipFileHeader)
	if err != nil {
		return fmt.Errorf("failed to create zip entry: %w", err)
	}

	file, err := os.Open(fi.path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	buf := make([]byte, defaultBufferSize)
	if _, err := io.CopyBuffer(writer, file, buf); err != nil {
		return fmt.Errorf("failed to write file to zip: %w", err)
	}

	progress.update(fi.info.Size())
	return nil
}

func chooseCompressionMethod(filePath string) uint16 {
	ext := strings.ToLower(filepath.Ext(filePath))

	// Danh sách các định dạng file đã được nén hoặc không nén hiệu quả
	noCompressionFormats := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
		".zip": true, ".rar": true, ".7z": true, ".gz": true,
		".bz2": true, ".xz": true, ".pdf": true, ".docx": true,
		".xlsx": true, ".pptx": true,
	}

	// Nếu file đã được nén hoặc không nén hiệu quả, sử dụng phương pháp STORE
	if noCompressionFormats[ext] {
		return zip.Store
	}

	// Đối với các file văn bản, sử dụng DEFLATE
	textFormats := map[string]bool{
		".txt": true, ".csv": true, ".md": true, ".json": true,
		".xml": true, ".html": true, ".css": true, ".js": true,
		".go": true, ".py": true, ".java": true, ".c": true,
		".cpp": true, ".h": true, ".sh": true, ".bat": true,
	}

	if textFormats[ext] {
		return zip.Deflate
	}

	// Đối với các file nhị phân khác, sử dụng một thuật toán nén mạnh hơn
	return zip.Deflate // Có thể thay bằng LZMA hoặc Bzip2 nếu thư viện zip hỗ trợ
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

func validateFile(path string, info os.FileInfo) error {
	if info.Size() == 0 {
		return fmt.Errorf("empty file detected: %s", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("potential directory traversal detected: %s", path)
	}
	return nil
}
