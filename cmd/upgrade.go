package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var Upgrade = &cobra.Command{
	Use:     "upgrade",
	Short:   "Self update the tool",
	Long:    "Check for updates and install the latest version of epubtrans",
	Example: "epubtrans upgrade",
	Version: "0.1.0",
	RunE:    runSelfUpgrade,
}

type GithubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func parseVersion(fullVersion string) (*semver.Version, error) {
	parts := strings.SplitN(fullVersion, "-", 2)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid version format")
	}
	versionStr := strings.TrimPrefix(parts[0], "v")
	return semver.NewVersion(versionStr)
}

func runSelfUpgrade(cmd *cobra.Command, args []string) error {
	currentVersion, err := parseVersion(Root.Version)
	if err != nil {
		return fmt.Errorf("invalid current version: %w", err)
	}

	cmd.Println("Checking for updates...")
	latestRelease, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion, err := semver.NewVersion(strings.TrimPrefix(latestRelease.TagName, "v"))
	if err != nil {
		return fmt.Errorf("invalid latest version: %w", err)
	}

	if !latestVersion.GreaterThan(currentVersion) {
		cmd.Println("You are already running the latest version.")
		return nil
	}

	cmd.Printf("Current version: %s\n", currentVersion)
	cmd.Printf("New version available: %s\n", latestVersion)
	cmd.Print("Do you want to update? (y/n): ")
	var response string
	fmt.Scanln(&response)
	if strings.ToLower(response) != "y" {
		cmd.Println("Upgrade cancelled.")
		return nil
	}

	if err := downloadAndInstall(cmd, latestRelease); err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}

	cmd.Println("Upgrade completed successfully. Please restart the application.")
	return nil
}

func getLatestRelease() (*GithubRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/nguyenvanduocit/epubtrans/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var release GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func downloadAndInstall(cmd *cobra.Command, release *GithubRelease) error {
	osArch := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	var assetURL string
	var assetName string

	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, osArch) {
			assetURL = asset.BrowserDownloadURL
			assetName = asset.Name
			break
		}
	}

	if assetURL == "" {
		return fmt.Errorf("no suitable asset found for %s", osArch)
	}

	cmd.Printf("Downloading epubtrans %s for %s...\n", release.TagName, osArch)
	tmpFile, err := downloadFile(cmd, assetURL)
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	cmd.Println("Verifying download...")
	if err := verifyChecksum(tmpFile.Name(), assetName, release); err != nil {
		return err
	}

	cmd.Println("Extracting...")
	if err := extractTarGz(cmd, tmpFile.Name()); err != nil {
		return err
	}

	cmd.Println("Installing...")
	if err := installBinary(); err != nil {
		return err
	}

	cmd.Printf("epubtrans %s has been installed successfully!\n", release.TagName)
	return nil
}

func downloadFile(cmd *cobra.Command, url string) (*os.File, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	tmpFile, err := os.CreateTemp("", "epubtrans_*.tar.gz")
	if err != nil {
		return nil, err
	}

	size := resp.ContentLength
	progress := 0
	progressBar := NewProgressBar(size)

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := tmpFile.Write(buf[:n])
			if writeErr != nil {
				return nil, writeErr
			}
			progress += n
			progressBar.Update(int64(progress))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	cmd.Println() // New line after progress bar
	return tmpFile, nil
}

func verifyChecksum(filePath, assetName string, release *GithubRelease) error {
	// Download the checksum file
	checksumURL := fmt.Sprintf("https://github.com/nguyenvanduocit/epubtrans/releases/download/%s/epubtrans_%s_checksums.txt", release.TagName, strings.TrimPrefix(release.TagName, "v"))
	resp, err := http.Get(checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksum file: %w", err)
	}
	defer resp.Body.Close()

	// Read and parse the checksum file
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) == 2 {
			checksums[parts[1]] = parts[0]
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read checksum file: %w", err)
	}

	// Get the expected checksum for our asset
	expectedChecksum, ok := checksums[assetName]
	if !ok {
		return fmt.Errorf("no checksum found for %s", assetName)
	}

	// Calculate the SHA256 checksum of the downloaded file
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	actualChecksum := hex.EncodeToString(h.Sum(nil))

	// Compare the checksums
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}

	return nil
}

func extractTarGz(cmd *cobra.Command, tarGzPath string) error {
	file, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if header.Typeflag == tar.TypeReg && header.Name == "epubtrans" {
			outFile, err := os.Create(filepath.Base(header.Name))
			if err != nil {
				return err
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, tr); err != nil {
				return err
			}

			if err := os.Chmod(outFile.Name(), 0755); err != nil {
				return err
			}

			break
		}
	}

	return nil
}

func installBinary() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Create a backup of the current executable
	backupPath := executable + ".bak"
	if err := copyFile(executable, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Copy the new binary to the executable path
	if err := copyFile("epubtrans", executable); err != nil {
		// If copy fails, try to restore the backup
		copyFile(backupPath, executable)
		os.Remove(backupPath)
		return fmt.Errorf("failed to install new version: %w", err)
	}

	// Remove the temporary extracted binary and the backup
	os.Remove("epubtrans")
	os.Remove(backupPath)

	return nil
}

func copyFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	if err != nil {
		return err
	}

	// Preserve the original file mode
	return os.Chmod(dst, sourceFileStat.Mode())
}

type ProgressBar struct {
	total int64
}

func NewProgressBar(total int64) *ProgressBar {
	return &ProgressBar{total: total}
}

func (pb *ProgressBar) Update(current int64) {
	// Simple progress bar implementation
	// This can be improved with a more sophisticated progress bar library
	percent := float64(current) / float64(pb.total) * 100
	fmt.Printf("\rProgress: %.2f%%", percent)
}
