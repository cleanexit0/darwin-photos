package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sudopromptr/photoscli/internal/db"
	"github.com/sudopromptr/photoscli/internal/models"
)

var (
	downloadOutput string
)

var downloadCmd = &cobra.Command{
	Use:   "download <uuid>",
	Short: "Download a photo from iCloud",
	Long: `Download a photo or video from your iCloud Photos library.

For locally available files, copies directly from the Photos library.
For cloud-only files, uses AppleScript to trigger Photos.app export
(which forces iCloud download).

Examples:
  photoscli download E448C88A -o ~/Downloads/
  photoscli download E448C88A-7F85-4746-AABE-BC594B79BD84`,
	Args: cobra.ExactArgs(1),
	RunE: runDownload,
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	downloadCmd.Flags().StringVarP(&downloadOutput, "output", "o", ".", "Output directory")
}

func runDownload(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	// Open database
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	// Find asset
	asset, err := db.GetAssetByUUID(photosDB.DB(), identifier)
	if err != nil {
		return fmt.Errorf("asset not found: %s", identifier)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(downloadOutput, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Determine output filename (use original filename if available)
	outputFilename := asset.OriginalFilename
	if outputFilename == "" {
		outputFilename = asset.Filename
	}
	outputPath := filepath.Join(downloadOutput, outputFilename)

	// Check if already exists
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("file already exists: %s", outputPath)
	}

	if asset.IsLocallyAvailable() {
		// Direct copy from library
		fmt.Printf("Copying local file: %s\n", asset.OriginalFilename)
		return copyLocalFile(asset, outputPath)
	}

	// Cloud-only: use AppleScript
	fmt.Printf("Downloading from iCloud via Photos.app: %s\n", outputFilename)
	return downloadViaAppleScript(asset.UUID, downloadOutput, outputFilename)
}

func copyLocalFile(asset *models.Asset, outputPath string) error {
	// Build source path
	dir := asset.Directory
	if dir == "" && len(asset.UUID) > 0 {
		dir = string(asset.UUID[0])
	}
	srcPath := filepath.Join(getLibraryPath(), "originals", dir, asset.Filename)

	// Check source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("source file not found: %s", srcPath)
	}

	// Copy file
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer dst.Close()

	bytes, err := io.Copy(dst, src)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	fmt.Printf("Copied %d bytes to %s\n", bytes, outputPath)
	return nil
}

func downloadViaAppleScript(uuid string, outputDir string, originalFilename string) error {
	// Convert to absolute path for AppleScript
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// AppleScript to export photo via Photos.app
	// This triggers iCloud download for cloud-only photos
	script := fmt.Sprintf(`
tell application "Photos"
	set mediaItem to media item id "%s"
	set outputFolder to POSIX file "%s"
	export {mediaItem} to outputFolder
end tell
`, uuid, absOutput)

	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("AppleScript export failed: %s: %w", string(output), err)
	}

	// Photos.app may change the extension (e.g., HEIC -> jpeg)
	fmt.Printf("Downloaded to %s/%s (Photos.app may change format)\n", absOutput, originalFilename)
	return nil
}
