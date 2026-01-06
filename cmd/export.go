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
	exportBatch   bool
	exportWorkers int
	exportLimit   int
	exportSkip    bool
)

var exportCmd = &cobra.Command{
	Use:   "export <uuid|--batch> <output-dir>",
	Short: "Export photos to a directory",
	Long: `Export photos (local or cloud) to a specified directory.

Uses PhotoKit to fetch the original image data. For cloud-only photos,
this triggers an iCloud download then exports.

Single export:
  photoscli export E448C88A ./output

Batch export (all cloud-only photos):
  photoscli export --batch ./output
  photoscli export --batch --workers 4 ./output
  photoscli export --batch --limit 100 ./output`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().BoolVar(&exportBatch, "batch", false, "Export all cloud-only photos")
	exportCmd.Flags().IntVarP(&exportWorkers, "workers", "w", 4, "Number of parallel workers for batch export")
	exportCmd.Flags().IntVarP(&exportLimit, "limit", "n", 0, "Limit number of photos to export (0 = unlimited)")
	exportCmd.Flags().BoolVar(&exportSkip, "skip-existing", false, "Skip files that already exist instead of erroring")
}

func runExport(cmd *cobra.Command, args []string) error {
	if exportBatch {
		if len(args) != 1 {
			return fmt.Errorf("batch mode requires exactly one argument: output directory")
		}
		return runBatchExport(args[0])
	}

	if len(args) != 2 {
		return fmt.Errorf("single export requires two arguments: uuid and output directory")
	}
	return runSingleExport(args[0], args[1])
}

func runSingleExport(identifier, outputDir string) error {
	// Validate output directory
	if err := validateOutputDir(outputDir); err != nil {
		return err
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

	// Build output path
	outputPath := buildOutputPath(outputDir, asset)

	// Check if file already exists
	if _, err := os.Stat(outputPath); err == nil {
		if exportSkip {
			fmt.Printf("Skipped (exists): %s\n", outputPath)
			return nil
		}
		return fmt.Errorf("file already exists: %s", outputPath)
	}

	// Export
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

func runBatchExport(outputDir string) error {
	// Validate output directory
	if err := validateOutputDir(outputDir); err != nil {
		return err
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

	// Get cloud-only assets
	opts := db.ListOptions{
		CloudOnly: true,
		Limit:     exportLimit,
	}
	assets, total, err := db.ListAssets(photosDB.DB(), opts)
	if err != nil {
		return fmt.Errorf("failed to list assets: %w", err)
	}

	if len(assets) == 0 {
		fmt.Println("No cloud-only photos to export")
		return nil
	}

	count := len(assets)
	if exportLimit > 0 && exportLimit < total {
		fmt.Printf("Exporting %d of %d cloud-only photos (limited)\n", count, total)
	} else {
		fmt.Printf("Exporting %d cloud-only photos\n", count)
	}
	fmt.Printf("Using %d workers\n\n", exportWorkers)

	// Create worker pool
	jobs := make(chan *models.Asset, count)
	results := make(chan exportResult, count)

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < exportWorkers; w++ {
		wg.Add(1)
		go exportWorker(outputDir, jobs, results, &wg)
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
	var completed, succeeded, failed, skipped int64
	startTime := time.Now()

	for result := range results {
		completed++
		if result.err != nil {
			atomic.AddInt64(&failed, 1)
			fmt.Printf("[%d/%d] FAILED: %s - %v\n", completed, count, result.filename, result.err)
		} else if result.skipped {
			atomic.AddInt64(&skipped, 1)
			fmt.Printf("[%d/%d] SKIPPED: %s\n", completed, count, result.filename)
		} else {
			atomic.AddInt64(&succeeded, 1)
			fmt.Printf("[%d/%d] OK: %s\n", completed, count, result.filename)
		}
	}

	// Summary
	elapsed := time.Since(startTime)
	fmt.Printf("\nCompleted in %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Succeeded: %d\n", succeeded)
	if skipped > 0 {
		fmt.Printf("  Skipped:   %d\n", skipped)
	}
	if failed > 0 {
		fmt.Printf("  Failed:    %d\n", failed)
	}

	if failed > 0 {
		return fmt.Errorf("%d exports failed", failed)
	}
	return nil
}

type exportResult struct {
	filename string
	skipped  bool
	err      error
}

func exportWorker(outputDir string, jobs <-chan *models.Asset, results chan<- exportResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for asset := range jobs {
		filename := asset.OriginalFilename
		if filename == "" {
			filename = asset.Filename
		}

		outputPath := buildOutputPath(outputDir, asset)

		// Check if exists
		if _, err := os.Stat(outputPath); err == nil {
			if exportSkip {
				results <- exportResult{filename: filename, skipped: true}
				continue
			}
			results <- exportResult{filename: filename, err: fmt.Errorf("file already exists")}
			continue
		}

		// Export
		if err := exportViaPhotoKit(asset, outputPath); err != nil {
			results <- exportResult{filename: filename, err: err}
			continue
		}

		results <- exportResult{filename: filename}
	}
}

func validateOutputDir(outputDir string) error {
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
	return nil
}

func buildOutputPath(outputDir string, asset *models.Asset) string {
	filename := asset.OriginalFilename
	if filename == "" {
		filename = asset.Filename
	}
	return filepath.Join(outputDir, filename)
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
