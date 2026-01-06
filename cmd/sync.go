package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/models"
	"github.com/cleanexit0/darwin-photos/internal/photokit"
)

var (
	syncAll     bool
	syncWorkers int
	syncLimit   int
	syncFile    string
)

var syncCmd = &cobra.Command{
	Use:   "sync [uuid] | --file <path> | - | --all",
	Short: "Sync cloud photos to the Photos library",
	Long: `Sync cloud-only photos from iCloud to the local Photos library.

Uses PhotoKit to trigger iCloud sync. The photos are stored in the
Photos library, making them available locally.

Single sync:
  darwin-photos sync E448C88A

From file (one UUID per line):
  darwin-photos sync --file uuids.txt

From stdin:
  cat uuids.txt | darwin-photos sync -
  darwin-photos sync - < uuids.txt

Sync all cloud-only photos:
  darwin-photos sync --all
  darwin-photos sync --all --workers 4
  darwin-photos sync --all --limit 100`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolVar(&syncAll, "all", false, "Sync all cloud-only photos")
	syncCmd.Flags().StringVarP(&syncFile, "file", "f", "", "File containing UUIDs (one per line)")
	syncCmd.Flags().IntVarP(&syncWorkers, "workers", "w", 4, "Number of parallel workers")
	syncCmd.Flags().IntVarP(&syncLimit, "limit", "n", 0, "Limit number of photos to sync (0 = unlimited)")
}

func runSync(cmd *cobra.Command, args []string) error {
	// All mode: all cloud-only photos
	if syncAll {
		if len(args) != 0 || syncFile != "" {
			return fmt.Errorf("--all cannot be combined with UUID or --file")
		}
		return runSyncAll()
	}

	// File input mode
	if syncFile != "" {
		if len(args) != 0 {
			return fmt.Errorf("--file cannot be combined with UUID argument")
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
		return fmt.Errorf("requires a UUID, --file, or --all")
	}
	return runSyncList([]string{args[0]})
}

func runSyncAll() error {
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

	// Get cloud-only assets
	opts := db.ListOptions{
		CloudOnly: true,
		Limit:     syncLimit,
	}
	assets, total, err := db.ListAssets(photosDB.DB(), opts)
	if err != nil {
		return fmt.Errorf("failed to list assets: %w", err)
	}

	if len(assets) == 0 {
		fmt.Println("No cloud-only photos to sync")
		return nil
	}

	if syncLimit > 0 && syncLimit < total {
		fmt.Printf("Syncing %d of %d cloud-only photos (limited)\n", len(assets), total)
	} else {
		fmt.Printf("Syncing %d cloud-only photos\n", len(assets))
	}

	return syncAssets(assets)
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

func readUUIDsFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return parseUUIDs(bufio.NewScanner(file))
}

func readUUIDsFromStdin() ([]string, error) {
	return parseUUIDs(bufio.NewScanner(os.Stdin))
}

func parseUUIDs(scanner *bufio.Scanner) ([]string, error) {
	var uuids []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		uuids = append(uuids, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}
	if len(uuids) == 0 {
		return nil, fmt.Errorf("no UUIDs found in input")
	}
	return uuids, nil
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
	var completed, succeeded, failed int64
	startTime := time.Now()

	for result := range results {
		completed++
		if result.err != nil {
			atomic.AddInt64(&failed, 1)
			fmt.Printf("[%d/%d] FAILED: %s - %v\n", completed, count, result.filename, result.err)
		} else {
			atomic.AddInt64(&succeeded, 1)
			fmt.Printf("[%d/%d] OK: %s\n", completed, count, result.filename)
		}
	}

	// Summary
	elapsed := time.Since(startTime)
	fmt.Printf("\nCompleted in %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Succeeded: %d\n", succeeded)
	if failed > 0 {
		fmt.Printf("  Failed:    %d\n", failed)
	}

	if failed > 0 {
		return fmt.Errorf("%d syncs failed", failed)
	}
	return nil
}
