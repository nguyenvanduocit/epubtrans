package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nguyenvanduocit/epubtrans/pkg/util"
	"github.com/spf13/cobra"
)

var Unpack = &cobra.Command{
	Use:     "unpack [unpackedEpubPath]",
	Short:   "Unpack an EPUB book into a directory",
	Long:    "This command unpacks an EPUB book file and creates a directory with the same name as the book, containing all the extracted contents. Ensure that the provided path points to a valid EPUB file.",
	Example: "epubtrans unpack path/to/book.epub",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("exactly one argument is required: the path to the EPUB file to unpack")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		zipPath := args[0]
		unzipPath, err := util.GetUnzipDestination(zipPath)
		if err != nil {
			return fmt.Errorf("failed to determine unzip destination: %w", err)
		}
		cmd.Println("Unzipping to:", unzipPath)
		if err := unzipBook(zipPath, unzipPath, func(format string, a ...interface{}) error {
			cmd.Printf(format, a...)
			return nil
		}); err != nil {
			return fmt.Errorf("failed to unzip book: %w", err)
		}

		cmd.Println("Unpacking completed successfully.")
		return nil
	},
}

func unzipBook(source, destination string, progress func(format string, a ...interface{}) error) error {
	r, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(destination, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	for _, f := range r.File {
		if err := extractFile(f, destination, progress); err != nil {
			return fmt.Errorf("failed to extract file %s: %w", f.Name, err)
		}
	}
	return nil
}

func extractFile(f *zip.File, destination string, progress func(format string, a ...interface{}) error) error {
	progress("Unzipping file: %s\n", f.Name)
	fpath := filepath.Join(destination, f.Name)

	if f.FileInfo().IsDir() {
		return os.MkdirAll(fpath, os.ModePerm)
	}

	if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
		return err
	}

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer outFile.Close()

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(outFile, rc)
	return err
}
