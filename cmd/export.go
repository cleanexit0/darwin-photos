package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/sudopromptr/photoscli/internal/db"
	"github.com/sudopromptr/photoscli/internal/models"
	"github.com/sudopromptr/photoscli/internal/photokit"
)

var exportCmd = &cobra.Command{
	Use:   "export <uuid> <output-dir>",
	Short: "Export a photo to a directory",
	Long: `Export a photo (local or cloud) to a specified directory.

Uses PhotoKit to fetch the original image data. For cloud-only photos,
this triggers an iCloud download then exports. Does NOT modify the Photos library.

Use 'download' instead if you want to sync a cloud photo to the library.

Examples:
  photoscli export E448C88A ./output
  photoscli export E448C88A ~/Desktop`,
	Args: cobra.ExactArgs(2),
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
}

func runExport(cmd *cobra.Command, args []string) error {
	identifier := args[0]
	outputDir := args[1]

	// Validate output directory
	info, err := os.Stat(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("output directory does not exist: %s", outputDir)
		}
		return fmt.Errorf("cannot access output directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output path is not a directory: %s", outputDir)
	}

	// Ensure Photos authorization
	if err := photokit.EnsureAuthorized(); err != nil {
		return err
	}

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

	// Build output path using original filename
	filename := asset.OriginalFilename
	if filename == "" {
		filename = asset.Filename
	}
	outputPath := filepath.Join(outputDir, filename)

	// Check if file already exists
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("file already exists: %s", outputPath)
	}

	// Export using PhotoKit
	fmt.Printf("Exporting: %s\n", asset.OriginalFilename)
	if !asset.IsLocallyAvailable() {
		fmt.Printf("  (downloading from iCloud...)\n")
	}

	if err := exportViaPhotoKit(asset, outputPath); err != nil {
		return err
	}

	fmt.Printf("Exported to: %s\n", outputPath)
	return nil
}

func exportViaPhotoKit(asset *models.Asset, outputPath string) error {
	// Convert UUID to PhotoKit local identifier
	localID := photokit.UUIDToLocalIdentifier(asset.UUID)

	// Download using PhotoKit
	if asset.Kind == models.AssetKindVideo {
		if err := photokit.DownloadVideoAsset(localID, outputPath); err != nil {
			return fmt.Errorf("export failed: %w", err)
		}
	} else {
		if err := photokit.DownloadAsset(localID, outputPath); err != nil {
			return fmt.Errorf("export failed: %w", err)
		}
	}

	return nil
}
