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
	"github.com/sudopromptr/photoscli/internal/db"
	"github.com/sudopromptr/photoscli/internal/models"
	"github.com/sudopromptr/photoscli/internal/photokit"
)

var (
	downloadBatch   bool
	downloadWorkers int
	downloadLimit   int
	downloadFile    string
)

var downloadCmd = &cobra.Command{
	Use:   "download [uuid] | --file <path> | - | --batch",
	Short: "Download cloud photos to the Photos library",
	Long: `Download cloud-only photos from iCloud to the Photos library.

Uses PhotoKit to trigger iCloud download. The photos are stored in the
Photos library, making them available locally.

Single download:
  photoscli download E448C88A

From file (one UUID per line):
  photoscli download --file uuids.txt

From stdin:
  cat uuids.txt | photoscli download -
  photoscli download - < uuids.txt

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
	downloadCmd.Flags().StringVarP(&downloadFile, "file", "f", "", "File containing UUIDs (one per line)")
	downloadCmd.Flags().IntVarP(&downloadWorkers, "workers", "w", 4, "Number of parallel workers")
	downloadCmd.Flags().IntVarP(&downloadLimit, "limit", "n", 0, "Limit number of photos to download (0 = unlimited)")
}

func runDownload(cmd *cobra.Command, args []string) error {
	// Batch mode: all cloud-only photos
	if downloadBatch {
		if len(args) != 0 || downloadFile != "" {
			return fmt.Errorf("--batch cannot be combined with UUID or --file")
		}
		return runBatchDownload()
	}

	// File input mode
	if downloadFile != "" {
		if len(args) != 0 {
			return fmt.Errorf("--file cannot be combined with UUID argument")
		}
		uuids, err := readUUIDsFromFile(downloadFile)
		if err != nil {
			return err
		}
		return runListDownload(uuids)
	}

	// Stdin mode
	if len(args) == 1 && args[0] == "-" {
		uuids, err := readUUIDsFromStdin()
		if err != nil {
			return err
		}
		return runListDownload(uuids)
	}

	// Single UUID mode
	if len(args) != 1 {
		return fmt.Errorf("requires a UUID, --file, or --batch")
	}
	return runListDownload([]string{args[0]})
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

	if downloadLimit > 0 && downloadLimit < total {
		fmt.Printf("Downloading %d of %d cloud-only photos (limited)\n", len(assets), total)
	} else {
		fmt.Printf("Downloading %d cloud-only photos\n", len(assets))
	}

	return downloadAssets(assets)
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

func runListDownload(identifiers []string) error {
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
		fmt.Println("No cloud-only photos to download")
		return nil
	}

	fmt.Printf("Downloading %d photos\n", len(assets))

	return downloadAssets(assets)
}

func downloadAssets(assets []*models.Asset) error {
	count := len(assets)
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
