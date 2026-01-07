package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/models"
	"github.com/cleanexit0/darwin-photos/internal/photokit"
	"github.com/spf13/cobra"
)

var (
	syncWorkers int
	syncFile    string
)

var syncCmd = &cobra.Command{
	Use:   "sync <uuid> | --from-file <path> | -",
	Short: "Sync cloud photos to the Photos library",
	Long: `Sync cloud-only photos from iCloud to the local Photos library.

Uses PhotoKit to trigger iCloud sync. The photos are stored in the
Photos library, making them available locally.

Single sync:
  darwin-photos sync E448C88A

From file (one UUID per line):
  darwin-photos sync --from-file uuids.txt

From stdin:
  darwin-photos ls --cloud-only | darwin-photos sync -

Note: Use 'sync' to download into your Photos library (uses local disk).
Use 'export' to download directly to any directory (e.g., external drive).
Use 'backup' for incremental backups to external storage.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().StringVarP(&syncFile, "from-file", "f", "", "File containing UUIDs (one per line)")
	syncCmd.Flags().IntVarP(&syncWorkers, "workers", "w", 16, "Number of parallel workers")
}

func runSync(cmd *cobra.Command, args []string) error {
	// File input mode
	if syncFile != "" {
		if len(args) != 0 {
			return fmt.Errorf("--from-file cannot be combined with UUID argument")
		}
		uuids, err := readUUIDsFromFile(syncFile)
		if err != nil {
			return err
		}
		return runSyncList(uuids)
	}

	// Stdin mode
	if len(args) == 1 && args[0] == "-" {
		uuids, err := readUUIDsFromStdin()
		if err != nil {
			return err
		}
		return runSyncList(uuids)
	}

	// Single UUID mode
	if len(args) != 1 {
		return fmt.Errorf("requires a UUID, --from-file, or -")
	}
	return runSyncList([]string{args[0]})
}

type syncResult struct {
	filename string
	err      error
}

func syncWorker(jobs <-chan *models.Asset, results chan<- syncResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for asset := range jobs {
		filename := asset.OriginalFilename
		if filename == "" {
			filename = asset.Filename
		}

		if err := syncViaPhotoKit(asset); err != nil {
			results <- syncResult{filename: filename, err: err}
			continue
		}

		results <- syncResult{filename: filename}
	}
}

func syncViaPhotoKit(asset *models.Asset) error {
	// Create temp directory for the sync
	tempDir, err := os.MkdirTemp("", "darwin-photos-sync-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	filename := asset.OriginalFilename
	if filename == "" {
		filename = asset.Filename
	}
	outputPath := filepath.Join(tempDir, filename)

	// Convert UUID to PhotoKit local identifier
	localID := photokit.UUIDToLocalIdentifier(asset.UUID)

	// Sync using PhotoKit (triggers iCloud download)
	if asset.Kind == models.AssetKindVideo {
		if err := photokit.DownloadVideoAsset(localID, outputPath); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
	} else {
		if err := photokit.DownloadAsset(localID, outputPath); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
	}

	return nil
}

func runSyncList(identifiers []string) error {
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

	// Look up assets
	var assets []*models.Asset
	var notFound []string
	for _, id := range identifiers {
		asset, err := db.GetAssetByUUID(photosDB.DB(), id)
		if err != nil {
			notFound = append(notFound, id)
			continue
		}
		if !asset.IsLocallyAvailable() {
			assets = append(assets, asset)
		}
	}

	if len(notFound) > 0 {
		fmt.Printf("Warning: %d UUIDs not found\n", len(notFound))
	}

	skippedLocal := len(identifiers) - len(notFound) - len(assets)
	if skippedLocal > 0 {
		fmt.Printf("Skipping %d already local photos\n", skippedLocal)
	}

	if len(assets) == 0 {
		fmt.Println("No cloud-only photos to sync")
		return nil
	}

	fmt.Printf("Syncing %d photos\n", len(assets))

	return syncAssets(assets)
}

func syncAssets(assets []*models.Asset) error {
	count := len(assets)
	fmt.Printf("Using %d workers\n\n", syncWorkers)

	// Create worker pool
	jobs := make(chan *models.Asset, count)
	results := make(chan syncResult, count)

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < syncWorkers; w++ {
		wg.Add(1)
		go syncWorker(jobs, results, &wg)
	}

	// Send jobs
	for _, asset := range assets {
		jobs <- asset
	}
	close(jobs)

	// Collect results in background
	go func() {
		wg.Wait()
		close(results)
	}()

	// Track progress
	var succeeded, failed int
	var failedItems []string
	startTime := time.Now()
	bar := NewProgressBar(count)

	for result := range results {
		bar.Add(1)
		if result.err != nil {
			failed++
			failedItems = append(failedItems, fmt.Sprintf("%s: %v", result.filename, result.err))
		} else {
			succeeded++
		}
	}
	bar.Finish()

	// Summary
	elapsed := time.Since(startTime)
	fmt.Printf("\nCompleted in %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Succeeded: %d\n", succeeded)
	if failed > 0 {
		fmt.Printf("  Failed:    %d\n", failed)
		for _, item := range failedItems {
			fmt.Printf("    - %s\n", item)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d syncs failed", failed)
	}
	return nil
}
