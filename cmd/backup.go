package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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

		if asset.IsLocallyAvailable() {
			localAssets = append(localAssets, asset)
			localBytes += asset.FileSize
		} else {
			cloudAssets = append(cloudAssets, asset)
			cloudBytes += asset.FileSize
		}
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
	if err == nil {
		if int64(freeSpace) < totalNeeded {
			return fmt.Errorf("insufficient disk space: need %s, have %s",
				formatBytes(totalNeeded), formatBytes(int64(freeSpace)))
		}
		fmt.Printf("Disk space: %s required, %s available\n\n", formatBytes(totalNeeded), formatBytes(int64(freeSpace)))
	}

	// Phase 1: Copy local files
	if len(localAssets) > 0 {
		fmt.Println("Copying local files...")
		if err := copyLocalFiles(localAssets, outputDir, localBytes); err != nil {
			return err
		}
		fmt.Println()
	}

	// Phase 2: Download cloud files
	if len(cloudAssets) > 0 {
		fmt.Println("Downloading from iCloud...")
		if err := downloadCloudFiles(cloudAssets, outputDir, cloudBytes); err != nil {
			return err
		}
	}

	fmt.Println("\nBackup complete!")
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
func copyLocalFiles(assets []*models.Asset, outputDir string, totalBytes int64) error {
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
	var succeeded, failed int64
	var failedItems []string

	for result := range results {
		bar.AddBytes(1, result.fileSize)
		if result.err != nil {
			failed++
			failedItems = append(failedItems, fmt.Sprintf("%s: %v", result.uuid, result.err))
		} else {
			succeeded++
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

	return nil
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
func downloadCloudFiles(assets []*models.Asset, outputDir string, totalBytes int64) error {
	// Get iCloud client
	client, err := getICloudClient()
	if err != nil {
		return err
	}

	// Open database for CloudKit GUID lookup
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	// Get UUIDs and look up CloudKit GUIDs
	uuids := make([]string, len(assets))
	for i, asset := range assets {
		uuids[i] = asset.UUID
	}

	guidMap, err := db.GetCloudMasterGUIDs(photosDB.DB(), uuids)
	if err != nil {
		return fmt.Errorf("failed to look up CloudKit GUIDs: %w", err)
	}

	// Build jobs for assets that have CloudKit GUIDs
	type cloudJob struct {
		asset        *models.Asset
		cloudKitGUID string
	}

	var validJobs []cloudJob
	var validTotalBytes int64
	for _, asset := range assets {
		if guid, ok := guidMap[asset.UUID]; ok {
			validJobs = append(validJobs, cloudJob{asset: asset, cloudKitGUID: guid})
			validTotalBytes += asset.FileSize
		} else {
			fmt.Printf("Warning: No CloudKit GUID found for %s (may be local-only)\n", asset.UUID)
		}
	}

	if len(validJobs) == 0 {
		fmt.Println("No cloud files to download")
		return nil
	}

	// Worker pool for downloads
	jobs := make(chan cloudJob, len(validJobs))
	results := make(chan exportResult, len(validJobs))

	var wg sync.WaitGroup
	for w := 0; w < backupWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				ext := filepath.Ext(job.asset.Filename)
				if ext == "" {
					ext = ".jpg"
				}
				outputPath := filepath.Join(outputDir, job.asset.UUID+ext)

				// Get download URL
				assetInfo, err := client.GetDownloadURL(job.cloudKitGUID)
				if err != nil {
					results <- exportResult{uuid: job.asset.UUID, err: fmt.Errorf("failed to get download URL: %w", err)}
					continue
				}

				// Download with retry
				if err := client.DownloadPhotoWithRetry(assetInfo.DownloadURL, outputPath, backupRetry); err != nil {
					results <- exportResult{uuid: job.asset.UUID, err: err}
					continue
				}

				results <- exportResult{uuid: job.asset.UUID, filename: outputPath, downloadBytes: job.asset.FileSize}
			}
		}()
	}

	// Send jobs
	for _, job := range validJobs {
		jobs <- job
	}
	close(jobs)

	// Collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	var succeeded, failed int64
	var failedUUIDs []string
	var failedItems []string
	bar := NewSizeProgressBar(len(validJobs), validTotalBytes)

	for result := range results {
		bar.AddBytes(1, result.downloadBytes)
		if result.err != nil {
			atomic.AddInt64(&failed, 1)
			failedUUIDs = append(failedUUIDs, result.uuid)
			failedItems = append(failedItems, fmt.Sprintf("%s: %v", result.uuid, result.err))
		} else {
			atomic.AddInt64(&succeeded, 1)
		}
	}
	bar.Finish()

	fmt.Printf("  Downloaded: %d\n", succeeded)
	if failed > 0 {
		fmt.Printf("  Failed: %d\n", failed)
		for _, item := range failedItems {
			fmt.Printf("    - %s\n", item)
		}

		// Write failed UUIDs for retry
		failedFile, err := writeFailedUUIDs(failedUUIDs)
		if err != nil {
			fmt.Printf("  Warning: could not write failed UUIDs to file: %v\n", err)
		} else {
			fmt.Printf("\nFailed UUIDs written to: %s\n", failedFile)
			fmt.Printf("Retry with: darwin-photos export --from-file %s %s\n", failedFile, outputDir)
		}
	}

	return nil
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
