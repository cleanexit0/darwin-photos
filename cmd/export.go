package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/icloud"
	"github.com/spf13/cobra"
)

var (
	exportWorkers int
	exportFile    string
	exportRetry   int
)

var exportCmd = &cobra.Command{
	Use:   "export <uuid> <output-dir>",
	Short: "Export photos directly from iCloud to a directory",
	Long: `Export photos directly from iCloud to a directory, bypassing local Photos library.

This downloads photos directly from iCloud servers, allowing you to export to
external storage without filling up local disk space.

Setup (import cookies from your browser):
  1. Log into icloud.com (or icloud.com.cn for China) in your browser
  2. Navigate to Photos to ensure photo cookies are loaded
  3. Export cookies using a browser extension:
     - Chrome: "Get cookies.txt LOCALLY" extension
     - Firefox: "cookies.txt" extension
     - Safari: "ExportCookies" from github.com/nickvdyck/ExportCookies
  4. Export cookies for "icloud.com" domain (Netscape/Cookies.txt format)
  5. Run: darwin-photos export import-cookies cookies.txt

Single export:
  darwin-photos export E448C88A /Volumes/Backup

From file (one UUID per line):
  darwin-photos export --from-file uuids.txt /Volumes/Backup

From stdin:
  darwin-photos ls --cloud-only | darwin-photos export - /Volumes/Backup

Note: Use 'export' to download directly to any directory (e.g., external drive).
Use 'sync' to download into your Photos library (uses local disk, no cookies needed).
Use 'backup' for incremental backups that handle both local and cloud photos.`,
	RunE: runExport,
}

var importCookiesCmd = &cobra.Command{
	Use:   "import-cookies <cookie-file>",
	Short: "Import browser cookies for iCloud authentication",
	Long: `Import cookies exported from your browser to authenticate with iCloud.

Steps:
  1. Log into icloud.com (or icloud.com.cn for China) in your browser
  2. Navigate to Photos to ensure photo cookies are loaded
  3. Export cookies using a browser extension:
     - Chrome: "Get cookies.txt LOCALLY" extension
     - Firefox: "cookies.txt" extension
     - Safari: "ExportCookies" from github.com/nickvdyck/ExportCookies
  4. Export cookies for "icloud.com" domain (Netscape/Cookies.txt format)
  5. Run: darwin-photos export import-cookies cookies.txt`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runImportCookies(args[0])
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear saved iCloud session",
	Long:  `Clear the saved iCloud session and cookies. You will need to import cookies again before exporting.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExportLogout()
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.AddCommand(importCookiesCmd)
	exportCmd.AddCommand(logoutCmd)

	exportCmd.Flags().StringVarP(&exportFile, "from-file", "f", "", "File containing UUIDs (one per line)")
	exportCmd.Flags().IntVarP(&exportWorkers, "workers", "w", 16, "Number of parallel workers")
	exportCmd.Flags().IntVarP(&exportRetry, "retry", "r", 3, "Number of retry attempts for failed downloads")
}

func runExport(cmd *cobra.Command, args []string) error {
	var outputDir string
	var uuids []string

	// File input mode
	if exportFile != "" {
		if len(args) != 1 {
			return fmt.Errorf("--from-file requires output directory")
		}
		outputDir = args[0]
		var err error
		uuids, err = readExportUUIDs(exportFile)
		if err != nil {
			return err
		}
		return runExportList(uuids, outputDir)
	}

	// Stdin mode
	if len(args) == 2 && args[0] == "-" {
		outputDir = args[1]
		var err error
		uuids, err = readExportUUIDsFromStdin()
		if err != nil {
			return err
		}
		return runExportList(uuids, outputDir)
	}

	// Single UUID mode
	if len(args) != 2 {
		return fmt.Errorf("requires UUID and output directory, or use --from-file/-")
	}
	return runExportList([]string{args[0]}, args[1])
}

func runExportLogout() error {
	sessionPath := icloud.DefaultSessionPath()
	if err := icloud.ClearSession(sessionPath); err != nil {
		return err
	}
	fmt.Println("Logged out successfully.")
	return nil
}

func runImportCookies(cookieFile string) error {
	// Parse cookie file (Netscape/Mozilla format)
	data, err := os.ReadFile(cookieFile)
	if err != nil {
		return fmt.Errorf("failed to read cookie file: %w", err)
	}

	content := string(data)

	// Parse ALL cookies from Netscape format
	// Format: domain	flag	path	secure	expiration	name	value
	var cookies []*icloud.ImportedCookie
	var hasToken bool

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 7 {
			domain := parts[0]
			path := parts[2]
			secure := parts[3] == "TRUE"
			name := parts[5]
			value := parts[6]

			// Strip quotes from cookie values (some exports include them)
			value = strings.Trim(value, `"`)

			// Only import cookies for iCloud domains
			if !strings.Contains(domain, "icloud") && !strings.Contains(domain, "apple") {
				continue
			}

			cookie := &icloud.ImportedCookie{
				Domain: domain,
				Path:   path,
				Secure: secure,
				Name:   name,
				Value:  value,
			}
			cookies = append(cookies, cookie)

			// Check if we have the main auth token
			if name == "X-APPLE-WEBAUTH-TOKEN" {
				hasToken = true
			}
		}
	}

	if len(cookies) == 0 {
		return fmt.Errorf("no iCloud cookies found in cookie file")
	}

	if !hasToken {
		return fmt.Errorf("X-APPLE-WEBAUTH-TOKEN not found in cookie file")
	}

	// Create client and import cookies
	client, err := icloud.NewClient()
	if err != nil {
		return err
	}

	// Import cookies first
	client.ImportCookies(cookies)

	// Discover Photos URL from iCloud
	fmt.Println("Discovering Photos URL from iCloud...")
	photosURL, err := client.DiscoverPhotosURL()
	if err != nil {
		return fmt.Errorf("failed to discover Photos URL: %w", err)
	}
	if photosURL == "" {
		return fmt.Errorf("no Photos URL found in response")
	}
	client.PhotosURL = photosURL

	// Save session
	sessionPath := icloud.DefaultSessionPath()
	if err := client.SaveSession(sessionPath); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	fmt.Println("Cookies imported successfully!")
	fmt.Printf("Session saved to: %s\n", sessionPath)

	return nil
}

func getICloudClient() (*icloud.Client, error) {
	client, err := icloud.NewClient()
	if err != nil {
		return nil, err
	}

	sessionPath := icloud.DefaultSessionPath()
	if err := client.LoadSession(sessionPath); err != nil {
		return nil, fmt.Errorf("not logged in. Run 'darwin-photos export import-cookies' first")
	}

	// Validate session with Apple servers before proceeding
	if _, err := client.DiscoverPhotosURL(); err != nil {
		return nil, fmt.Errorf("session validation failed: %w\nRun 'darwin-photos export import-cookies' with fresh cookies", err)
	}

	return client, nil
}

func runExportList(uuids []string, outputDir string) error {
	if err := validateExportDir(outputDir); err != nil {
		return err
	}

	client, err := getICloudClient()
	if err != nil {
		return err
	}

	// Look up file sizes from database
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	sizeMap, totalBytes, err := db.GetAssetSizes(photosDB.DB(), uuids)
	if err != nil {
		return fmt.Errorf("failed to look up file sizes: %w", err)
	}

	fmt.Printf("Exporting %d photos (%s)\n", len(uuids), formatBytes(totalBytes))
	return exportPhotos(client, uuids, sizeMap, outputDir)
}

type exportJob struct {
	uuid         string
	cloudKitGUID string
	fileSize     int64
}

type exportResult struct {
	uuid          string
	filename      string
	skipped       bool
	downloadBytes int64
	err           error
}

type exportConfig struct {
	workers    int
	maxRetries int
	outputDir  string
}

func exportPhotos(client *icloud.Client, uuids []string, sizeMap map[string]int64, outputDir string) error {
	config := &exportConfig{
		workers:    exportWorkers,
		maxRetries: exportRetry,
		outputDir:  outputDir,
	}
	failedUUIDs, err := downloadFromCloud(client, uuids, sizeMap, config)
	if err != nil {
		return err
	}

	if len(failedUUIDs) > 0 {
		// Write failed UUIDs to ~/.darwin-photos/ with timestamp
		failedFile, err := writeFailedUUIDs(failedUUIDs)
		if err != nil {
			fmt.Printf("  Warning: could not write failed UUIDs to file: %v\n", err)
		} else {
			fmt.Printf("\nFailed UUIDs written to: %s\n", failedFile)
			fmt.Printf("Retry with: darwin-photos export --from-file %s %s\n", failedFile, outputDir)
		}
		return fmt.Errorf("%d exports failed", len(failedUUIDs))
	}
	return nil
}

// downloadFromCloud downloads photos from iCloud and returns list of failed UUIDs.
// This is shared between export and backup commands.
func downloadFromCloud(client *icloud.Client, uuids []string, sizeMap map[string]int64, config *exportConfig) ([]string, error) {
	// Open Photos database to look up CloudKit GUIDs
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return nil, fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	// Map UUIDs to CloudKit GUIDs
	guidMap, err := db.GetCloudMasterGUIDs(photosDB.DB(), uuids)
	if err != nil {
		return nil, fmt.Errorf("failed to look up CloudKit GUIDs: %w", err)
	}

	// Filter to only UUIDs that have CloudKit GUIDs
	var validJobs []exportJob
	var validTotalBytes int64
	for _, uuid := range uuids {
		if guid, ok := guidMap[uuid]; ok {
			fileSize := sizeMap[uuid]
			validJobs = append(validJobs, exportJob{uuid: uuid, cloudKitGUID: guid, fileSize: fileSize})
			validTotalBytes += fileSize
		} else {
			fmt.Printf("Warning: No CloudKit GUID found for %s (may be local-only)\n", uuid)
		}
	}

	if len(validJobs) == 0 {
		fmt.Println("No cloud files to download")
		return nil, nil
	}

	count := len(validJobs)

	jobs := make(chan exportJob, count)
	results := make(chan exportResult, count)

	var wg sync.WaitGroup
	for w := 0; w < config.workers; w++ {
		wg.Add(1)
		go exportWorker(client, config, jobs, results, &wg)
	}

	for _, job := range validJobs {
		jobs <- job
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var succeeded, failed, skipped int
	var failedUUIDs []string
	var failedItems []string
	startTime := time.Now()
	bar := NewSizeProgressBar(count, validTotalBytes)

	for result := range results {
		if result.err != nil {
			failed++
			failedUUIDs = append(failedUUIDs, result.uuid)
			failedItems = append(failedItems, fmt.Sprintf("%s: %v", result.uuid, result.err))
			bar.AddBytes(1, 0) // Don't count failed bytes in progress
		} else if result.skipped {
			skipped++
			bar.AddBytes(1, result.downloadBytes)
		} else {
			succeeded++
			bar.AddBytes(1, result.downloadBytes)
		}
	}
	bar.Finish()

	elapsed := time.Since(startTime)
	fmt.Printf("\nCompleted in %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Succeeded: %d\n", succeeded)
	if skipped > 0 {
		fmt.Printf("  Skipped:   %d (already exist in output directory)\n", skipped)
	}
	if failed > 0 {
		fmt.Printf("  Failed:    %d\n", failed)
		for _, item := range failedItems {
			fmt.Printf("    - %s\n", item)
		}
	}

	return failedUUIDs, nil
}

func writeFailedUUIDs(uuids []string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	dir := filepath.Join(home, ".darwin-photos")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Use timestamp for unique filename: failed_exports_20060102_150405.txt
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("failed_exports_%s.txt", timestamp)
	path := filepath.Join(dir, filename)

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	for _, uuid := range uuids {
		if _, err := fmt.Fprintln(file, uuid); err != nil {
			return "", err
		}
	}
	return path, nil
}

func exportWorker(client *icloud.Client, config *exportConfig, jobs <-chan exportJob, results chan<- exportResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		// Get download URL from iCloud using CloudKit GUID
		asset, err := client.GetDownloadURL(job.cloudKitGUID)
		if err != nil {
			results <- exportResult{uuid: job.uuid, err: fmt.Errorf("failed to get download URL: %w", err)}
			continue
		}

		// Use UUID as filename, preserving original extension
		ext := filepath.Ext(asset.Filename)
		if ext == "" {
			ext = ".jpg" // Default extension if none available
		}
		filename := job.uuid + ext

		outputPath := filepath.Join(config.outputDir, filename)

		// Check if file exists - skip if already present
		if _, err := os.Stat(outputPath); err == nil {
			results <- exportResult{uuid: job.uuid, filename: filename, skipped: true, downloadBytes: job.fileSize}
			continue
		}

		// Download with retry
		if err := client.DownloadPhotoWithRetry(asset.DownloadURL, outputPath, config.maxRetries); err != nil {
			results <- exportResult{uuid: job.uuid, err: err}
			continue
		}

		results <- exportResult{uuid: job.uuid, filename: filename, downloadBytes: job.fileSize}
	}
}

func validateExportDir(outputDir string) error {
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

func readExportUUIDs(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return parseExportUUIDs(bufio.NewScanner(file))
}

func readExportUUIDsFromStdin() ([]string, error) {
	return parseExportUUIDs(bufio.NewScanner(os.Stdin))
}

func parseExportUUIDs(scanner *bufio.Scanner) ([]string, error) {
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
