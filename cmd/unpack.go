package cmd

import (
	"archive/zip"
	"epubtrans/pkg/util"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
)

var Unpack = &cobra.Command{
	Use:   "unpack",
	Short: "unpack a book",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		zipPath := args[0]

		if err := unzipBook(zipPath, util.GetUnzipPath(zipPath)); err != nil {
			return err
		}

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
