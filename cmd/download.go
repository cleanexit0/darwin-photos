package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sudopromptr/photoscli/internal/db"
	"github.com/sudopromptr/photoscli/internal/models"
)

var downloadCmd = &cobra.Command{
	Use:   "download <uuid>",
	Short: "Download a photo from iCloud to the library",
	Long: `Download a cloud-only photo to its proper location in the Photos library.

For cloud-only files, triggers Photos.app to download from iCloud.
The file will be available at its library path (e.g., originals/E/UUID.heic).

Examples:
  photoscli download E448C88A`,
	Args: cobra.ExactArgs(1),
	RunE: runDownload,
}

func init() {
	rootCmd.AddCommand(downloadCmd)
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

	// Build library path
	libraryPath := buildLibraryPath(asset)

	if asset.IsLocallyAvailable() {
		fmt.Printf("Already downloaded: %s\n", libraryPath)
		return nil
	}

	// Cloud-only: trigger download via AppleScript
	fmt.Printf("Downloading from iCloud: %s\n", asset.OriginalFilename)

	if err := triggerDownloadToLibrary(asset.UUID); err != nil {
		return err
	}

	fmt.Printf("Downloaded to library: %s\n", libraryPath)
	return nil
}

func buildLibraryPath(asset *models.Asset) string {
	dir := asset.Directory
	if dir == "" && len(asset.UUID) > 0 {
		dir = string(asset.UUID[0])
	}
	return filepath.Join(getLibraryPath(), "originals", dir, asset.Filename)
}

func triggerDownloadToLibrary(uuid string) error {
	// Export to temp directory to trigger iCloud download, then delete the export
	tempDir, err := os.MkdirTemp("", "photoscli-download-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	return triggerDownloadViaExport(uuid, tempDir)
}

func triggerDownloadViaExport(uuid string, outputDir string) error {
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

	return nil
}
