package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/models"
	"github.com/spf13/cobra"
)

var (
	backupWorkers int
	backupRetry   int
	backupStart   string
	backupEnd     string
)

var backupCmd = &cobra.Command{
	Use:   "backup <output-dir>",
	Short: "Incrementally backup all photos to a directory",
	Long: `Backup all photos to a directory, handling both local and cloud-only files.

This command performs an incremental backup:
  1. Scans your Photos library to find all photos
  2. Scans the output directory to find already-backed-up photos
  3. Copies local photos directly (fast, no network needed)
  4. Downloads cloud-only photos from iCloud (requires cookies)

The backup is idempotent - running it again will only copy/download
missing files. Files are named by UUID to ensure uniqueness.

Examples:
  darwin-photos backup /Volumes/Backup/Photos
  darwin-photos backup --start 2024-01-01 /Volumes/Backup/Photos
  darwin-photos backup --workers 8 /Volumes/Backup/Photos

Note: For cloud-only photos, you need to set up iCloud cookies first:
  darwin-photos export import-cookies cookies.txt`,
	Args: cobra.ExactArgs(1),
	RunE: runBackup,
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.Flags().IntVarP(&backupWorkers, "workers", "w", 16, "Number of parallel workers for cloud downloads")
	backupCmd.Flags().IntVarP(&backupRetry, "retry", "r", 3, "Number of retry attempts for failed downloads")
	backupCmd.Flags().StringVar(&backupStart, "start", "", "Start date (YYYY-MM-DD), inclusive")
	backupCmd.Flags().StringVar(&backupEnd, "end", "", "End date (YYYY-MM-DD), inclusive")
}

func runBackup(cmd *cobra.Command, args []string) error {
	outputDir := args[0]

	// Parse date filters
	var startDate, endDate time.Time
	var err error
	if backupStart != "" {
		startDate, err = time.Parse("2006-01-02", backupStart)
		if err != nil {
			return fmt.Errorf("invalid start date %q: use YYYY-MM-DD format", backupStart)
		}
	}
	if backupEnd != "" {
		endDate, err = time.Parse("2006-01-02", backupEnd)
		if err != nil {
			return fmt.Errorf("invalid end date %q: use YYYY-MM-DD format", backupEnd)
		}
	}

	// Validate output directory
	if err := validateExportDir(outputDir); err != nil {
		return err
	}

	// Open database
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	// Get all assets from library
	fmt.Print("Scanning library...")
	opts := db.ListOptions{
		StartDate: startDate,
		EndDate:   endDate,
	}
	assets, _, err := db.ListAssets(photosDB.DB(), opts)
	if err != nil {
		return fmt.Errorf("failed to list assets: %w", err)
	}
	fmt.Printf(" %d photos\n", len(assets))

	if len(assets) == 0 {
		fmt.Println("No photos to backup")
		return nil
	}

	// Scan output directory to find already backed up files
	fmt.Print("Scanning output directory...")
	existingFiles, err := scanBackupDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to scan output directory: %w", err)
	}
	fmt.Printf(" %d already backed up\n", len(existingFiles))

	// Categorize assets: already backed up, local, cloud-only
	var localAssets, cloudAssets []*models.Asset
	var localBytes, cloudBytes int64

	for _, asset := range assets {
		// Build expected filename (UUID + extension)
		ext := filepath.Ext(asset.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		expectedFile := asset.UUID + ext

		if existingFiles[expectedFile] {
			continue // Already backed up
		}

		// Check if file actually exists on disk (database can be out of sync)
		if asset.IsLocallyAvailable() {
			if _, err := os.Stat(buildLocalPath(asset)); err == nil {
				localAssets = append(localAssets, asset)
				localBytes += asset.FileSize
				continue
			}
			// File doesn't exist despite database saying it's local - fall through to cloud
		}

		cloudAssets = append(cloudAssets, asset)
		cloudBytes += asset.FileSize
	}

	// Print summary
	fmt.Println()
	if len(localAssets) == 0 && len(cloudAssets) == 0 {
		fmt.Println("All photos already backed up!")
		return nil
	}

	fmt.Printf("Missing: %d photos\n", len(localAssets)+len(cloudAssets))
	if len(localAssets) > 0 {
		fmt.Printf("  - %d available locally (%s)\n", len(localAssets), formatBytes(localBytes))
	}
	if len(cloudAssets) > 0 {
		fmt.Printf("  - %d cloud-only (%s)\n", len(cloudAssets), formatBytes(cloudBytes))
	}
	fmt.Println()

	// Check disk space
	totalNeeded := localBytes + cloudBytes
	freeSpace, err := getFreeDiskSpace(outputDir)
	if err != nil {
		fmt.Printf("Warning: could not check disk space: %v\n\n", err)
	} else {
		if int64(freeSpace) < totalNeeded {
			return fmt.Errorf("insufficient disk space: need %s, have %s",
				formatBytes(totalNeeded), formatBytes(int64(freeSpace)))
		}
		fmt.Printf("Disk space: %s required, %s available\n\n", formatBytes(totalNeeded), formatBytes(int64(freeSpace)))
	}

	startTime := time.Now()
	var allFailedUUIDs []string

	// Phase 1: Copy local files
	if len(localAssets) > 0 {
		fmt.Println("Copying local files...")
		failedUUIDs, err := copyLocalFiles(localAssets, outputDir, localBytes)
		if err != nil {
			return err
		}
		allFailedUUIDs = append(allFailedUUIDs, failedUUIDs...)
		fmt.Println()
	}

	// Phase 2: Download cloud files
	if len(cloudAssets) > 0 {
		fmt.Println("Downloading from iCloud...")
		failedUUIDs, err := downloadCloudFiles(cloudAssets, outputDir)
		if err != nil {
			return err
		}
		allFailedUUIDs = append(allFailedUUIDs, failedUUIDs...)
	}

	// Write all failed UUIDs to file if any
	if len(allFailedUUIDs) > 0 {
		failedFile, err := writeFailedUUIDs(allFailedUUIDs)
		if err != nil {
			fmt.Printf("\nWarning: could not write failed UUIDs to file: %v\n", err)
		} else {
			fmt.Printf("\nFailed UUIDs written to: %s\n", failedFile)
			fmt.Printf("Retry with: darwin-photos backup %s\n", outputDir)
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\nBackup complete! (took %s)\n", elapsed.Round(time.Second))
	return nil
}

// scanBackupDir returns a set of filenames already in the backup directory
func scanBackupDir(dir string) (map[string]bool, error) {
	files := make(map[string]bool)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			files[entry.Name()] = true
		}
	}

	return files, nil
}

// copyLocalFiles copies locally available files to the output directory using parallel workers
// Returns a list of failed UUIDs for retry
func copyLocalFiles(assets []*models.Asset, outputDir string, totalBytes int64) ([]string, error) {
	type copyJob struct {
		asset   *models.Asset
		srcPath string
		dstPath string
	}

	type copyResult struct {
		uuid      string
		fileSize  int64
		err       error
	}

	// Use same number of workers as backup
	numWorkers := backupWorkers
	jobs := make(chan copyJob, len(assets))
	results := make(chan copyResult, len(assets))

	// Start workers
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				err := copyFile(job.srcPath, job.dstPath)
				results <- copyResult{
					uuid:     job.asset.UUID,
					fileSize: job.asset.FileSize,
					err:      err,
				}
			}
		}()
	}

	// Send jobs
	for _, asset := range assets {
		srcPath := buildLocalPath(asset)
		ext := filepath.Ext(asset.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		dstPath := filepath.Join(outputDir, asset.UUID+ext)
		jobs <- copyJob{asset: asset, srcPath: srcPath, dstPath: dstPath}
	}
	close(jobs)

	// Collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	bar := NewSizeProgressBar(len(assets), totalBytes)
	var succeeded, failed int
	var failedUUIDs []string
	var failedItems []string

	for result := range results {
		if result.err != nil {
			failed++
			failedUUIDs = append(failedUUIDs, result.uuid)
			failedItems = append(failedItems, fmt.Sprintf("%s: %v", result.uuid, result.err))
			bar.AddBytes(1, 0) // Don't count failed bytes in progress
		} else {
			succeeded++
			bar.AddBytes(1, result.fileSize)
		}
	}
	bar.Finish()

	fmt.Printf("  Copied: %d\n", succeeded)
	if failed > 0 {
		fmt.Printf("  Failed: %d\n", failed)
		for _, item := range failedItems {
			fmt.Printf("    - %s\n", item)
		}
	}

	return failedUUIDs, nil
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst) // Clean up partial file
		return err
	}

	return dstFile.Sync()
}

// downloadCloudFiles downloads cloud-only files from iCloud
// Returns a list of failed UUIDs for retry
func downloadCloudFiles(assets []*models.Asset, outputDir string) ([]string, error) {
	// Get iCloud client
	client, err := getICloudClient()
	if err != nil {
		return nil, err
	}

	// Build UUID list and size map
	uuids := make([]string, len(assets))
	sizeMap := make(map[string]int64)
	for i, asset := range assets {
		uuids[i] = asset.UUID
		sizeMap[asset.UUID] = asset.FileSize
	}

	config := &exportConfig{
		workers:    backupWorkers,
		maxRetries: backupRetry,
		outputDir:  outputDir,
	}

	return downloadFromCloud(client, uuids, sizeMap, config)
}

// getFreeDiskSpace returns available disk space in bytes for the given path
func getFreeDiskSpace(path string) (uint64, error) {
	// Use syscall to get disk space info
	// This is macOS-specific
	var stat syscallStatfs
	if err := statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
