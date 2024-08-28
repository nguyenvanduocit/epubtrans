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

		cmd.Println("Unzipping to:", unzipPath)
		if err != nil {
			return err
		}
		if err := unzipBook(zipPath, unzipPath); err != nil {
			return err
		}

		cmd.Println("Unpacking completed successfully.")
		return nil
	},
}

func unzipBook(source, destination string) error {
	r, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer r.Close()

	// Ensure the destination directory exists
	if err := os.MkdirAll(destination, os.ModePerm); err != nil {
		return err
	}

	for _, f := range r.File {
		fmt.Println("Unzipping file:", f.Name)
		fpath := filepath.Join(destination, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)

		// Close the file without masking the previous error
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}
