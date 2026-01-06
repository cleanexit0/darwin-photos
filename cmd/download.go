package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/sudopromptr/photoscli/internal/db"
	"github.com/sudopromptr/photoscli/internal/models"
	"github.com/sudopromptr/photoscli/internal/photokit"
)

var (
	downloadBatch   bool
	downloadWorkers int
	downloadLimit   int
)

var downloadCmd = &cobra.Command{
	Use:   "download <uuid|--batch>",
	Short: "Download cloud photos to the Photos library",
	Long: `Download cloud-only photos from iCloud to the Photos library.

Uses PhotoKit to trigger iCloud download. The photos are stored in the
Photos library, making them available locally.

Single download:
  photoscli download E448C88A

Batch download (all cloud-only photos):
  photoscli download --batch
  photoscli download --batch --workers 4
  photoscli download --batch --limit 100`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDownload,
}

func init() {
	rootCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().BoolVar(&downloadBatch, "batch", false, "Download all cloud-only photos")
	downloadCmd.Flags().IntVarP(&downloadWorkers, "workers", "w", 4, "Number of parallel workers for batch download")
	downloadCmd.Flags().IntVarP(&downloadLimit, "limit", "n", 0, "Limit number of photos to download (0 = unlimited)")
}

func runDownload(cmd *cobra.Command, args []string) error {
	if downloadBatch {
		if len(args) != 0 {
			return fmt.Errorf("batch mode does not accept a UUID argument")
		}
		return runBatchDownload()
	}

	if len(args) != 1 {
		return fmt.Errorf("single download requires a UUID argument")
	}
	return runSingleDownload(args[0])
}

func runSingleDownload(identifier string) error {
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

	if asset.IsLocallyAvailable() {
		fmt.Printf("Already available locally: %s\n", asset.OriginalFilename)
		return nil
	}

	// Download
	fmt.Printf("Downloading from iCloud: %s\n", asset.OriginalFilename)

	if err := downloadViaPhotoKit(asset); err != nil {
		return err
	}

	fmt.Printf("Downloaded: %s\n", asset.OriginalFilename)
	return nil
}

func runBatchDownload() error {
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
		Limit:     downloadLimit,
	}
	assets, total, err := db.ListAssets(photosDB.DB(), opts)
	if err != nil {
		return fmt.Errorf("failed to list assets: %w", err)
	}

	if len(assets) == 0 {
		fmt.Println("No cloud-only photos to download")
		return nil
	}

	count := len(assets)
	if downloadLimit > 0 && downloadLimit < total {
		fmt.Printf("Downloading %d of %d cloud-only photos (limited)\n", count, total)
	} else {
		fmt.Printf("Downloading %d cloud-only photos\n", count)
	}
	fmt.Printf("Using %d workers\n\n", downloadWorkers)

	// Create worker pool
	jobs := make(chan *models.Asset, count)
	results := make(chan downloadResult, count)

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < downloadWorkers; w++ {
		wg.Add(1)
		go downloadWorker(jobs, results, &wg)
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
		return fmt.Errorf("%d downloads failed", failed)
	}
	return nil
}

type downloadResult struct {
	filename string
	err      error
}

func downloadWorker(jobs <-chan *models.Asset, results chan<- downloadResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for asset := range jobs {
		filename := asset.OriginalFilename
		if filename == "" {
			filename = asset.Filename
		}

		// Download
		if err := downloadViaPhotoKit(asset); err != nil {
			results <- downloadResult{filename: filename, err: err}
			continue
		}

		results <- downloadResult{filename: filename}
	}
}

func downloadViaPhotoKit(asset *models.Asset) error {
	// Create temp directory for the download
	tempDir, err := os.MkdirTemp("", "photoscli-download-*")
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

	// Download using PhotoKit (triggers iCloud download)
	if asset.Kind == models.AssetKindVideo {
		if err := photokit.DownloadVideoAsset(localID, outputPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		if err := photokit.DownloadAsset(localID, outputPath); err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	}

	return nil
}
