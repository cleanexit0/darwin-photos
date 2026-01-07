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

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/icloud"
	"github.com/spf13/cobra"
)

var (
	exportAll     bool
	exportWorkers int
	exportLimit   int
	exportFile    string
	exportRetry   int
)

var exportCmd = &cobra.Command{
	Use:   "export [uuid] | --from-file <path> | - | --all <output-dir>",
	Short: "Export photos directly from iCloud to a directory",
	Long: `Export photos directly from iCloud to a directory, bypassing local Photos library.

This downloads photos directly from iCloud servers, allowing you to export to
external storage without filling up local disk space.

Setup (import cookies from your browser):
  1. Log into icloud.com or icloud.com.cn in Chrome
  2. Use a cookie export extension to save cookies (Netscape format)
  3. Run: darwin-photos export import-cookies cookies.txt

Commands:
  darwin-photos export import-cookies <cookie-file>  # Import browser cookies
  darwin-photos export logout                        # Clear saved session

Single export:
  darwin-photos export E448C88A /Volumes/Backup

From file (one UUID per line):
  darwin-photos export --from-file uuids.txt /Volumes/Backup

From stdin:
  cat uuids.txt | darwin-photos export - /Volumes/Backup

Export all cloud-only photos:
  darwin-photos export --all /Volumes/Backup
  darwin-photos export --all --workers 4 /Volumes/Backup
  darwin-photos export --all --limit 100 /Volumes/Backup

Note: Use 'export' to download directly to any directory (e.g., external drive).
Use 'sync' to download into your Photos library (uses local disk, no cookies needed).`,
	Args: cobra.MinimumNArgs(1),
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().BoolVar(&exportAll, "all", false, "Export all cloud-only photos")
	exportCmd.Flags().StringVarP(&exportFile, "from-file", "f", "", "File containing UUIDs (one per line)")
	exportCmd.Flags().IntVarP(&exportWorkers, "workers", "w", 4, "Number of parallel workers")
	exportCmd.Flags().IntVarP(&exportLimit, "limit", "n", 0, "Limit number of photos to export (0 = unlimited)")
	exportCmd.Flags().IntVarP(&exportRetry, "retry", "r", 3, "Number of retry attempts for failed downloads")
}

func runExport(cmd *cobra.Command, args []string) error {
	// Handle subcommands
	if len(args) >= 1 {
		switch args[0] {
		case "logout":
			return runExportLogout()
		case "import-cookies":
			if len(args) < 2 {
				return fmt.Errorf("import-cookies requires: <cookie-file>\nExample: darwin-photos export import-cookies cookies.txt")
			}
			return runImportCookies(args[1])
		}
	}

	// All other modes need an output directory
	var outputDir string
	var uuids []string

	if exportAll {
		if len(args) != 1 {
			return fmt.Errorf("--all requires output directory")
		}
		outputDir = args[0]
		return runExportAll(outputDir)
	}

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
		return fmt.Errorf("requires UUID and output directory, or use --all/--from-file")
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
		return nil, fmt.Errorf("not logged in. Run 'darwin-photos export login' first")
	}

	if !client.IsLoggedIn() {
		return nil, fmt.Errorf("session expired. Run 'darwin-photos export login' to re-authenticate")
	}

	return client, nil
}

func runExportAll(outputDir string) error {
	if err := validateExportDir(outputDir); err != nil {
		return err
	}

	client, err := getICloudClient()
	if err != nil {
		return err
	}

	// Get cloud-only assets from local database
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

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

	// Extract UUIDs
	uuids := make([]string, len(assets))
	for i, asset := range assets {
		uuids[i] = asset.UUID
	}

	if exportLimit > 0 && exportLimit < total {
		fmt.Printf("Exporting %d of %d cloud-only photos (limited)\n", len(uuids), total)
	} else {
		fmt.Printf("Exporting %d cloud-only photos\n", len(uuids))
	}

	return exportPhotos(client, uuids, outputDir)
}

func runExportList(uuids []string, outputDir string) error {
	if err := validateExportDir(outputDir); err != nil {
		return err
	}

	client, err := getICloudClient()
	if err != nil {
		return err
	}

	fmt.Printf("Exporting %d photos\n", len(uuids))
	return exportPhotos(client, uuids, outputDir)
}

type exportJob struct {
	uuid         string
	cloudKitGUID string
}

type exportResult struct {
	uuid     string
	filename string
	skipped  bool
	err      error
}

type exportConfig struct {
	maxRetries int
	outputDir  string
}

func exportPhotos(client *icloud.Client, uuids []string, outputDir string) error {
	// Open Photos database to look up CloudKit GUIDs
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	// Map UUIDs to CloudKit GUIDs
	fmt.Println("Looking up CloudKit GUIDs...")
	guidMap, err := db.GetCloudMasterGUIDs(photosDB.DB(), uuids)
	if err != nil {
		return fmt.Errorf("failed to look up CloudKit GUIDs: %w", err)
	}

	// Filter to only UUIDs that have CloudKit GUIDs
	var validJobs []exportJob
	for _, uuid := range uuids {
		if guid, ok := guidMap[uuid]; ok {
			validJobs = append(validJobs, exportJob{uuid: uuid, cloudKitGUID: guid})
		} else {
			fmt.Printf("Warning: No CloudKit GUID found for %s (may be local-only)\n", uuid)
		}
	}

	if len(validJobs) == 0 {
		return fmt.Errorf("no valid CloudKit GUIDs found for the given UUIDs")
	}

	count := len(validJobs)
	fmt.Printf("Found %d photos with CloudKit GUIDs\n", count)
	fmt.Printf("Using %d workers\n", exportWorkers)
	if exportRetry > 0 {
		fmt.Printf("Retries: %d per photo\n", exportRetry)
	}
	fmt.Println()

	config := &exportConfig{
		maxRetries: exportRetry,
		outputDir:  outputDir,
	}

	jobs := make(chan exportJob, count)
	results := make(chan exportResult, count)

	var wg sync.WaitGroup
	for w := 0; w < exportWorkers; w++ {
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

	var completed, succeeded, failed, skipped int64
	var failedUUIDs []string
	startTime := time.Now()

	for result := range results {
		completed++
		if result.err != nil {
			atomic.AddInt64(&failed, 1)
			failedUUIDs = append(failedUUIDs, result.uuid)
			fmt.Printf("[%d/%d] FAILED: %s - %v\n", completed, count, result.uuid, result.err)
		} else if result.skipped {
			atomic.AddInt64(&skipped, 1)
			fmt.Printf("[%d/%d] SKIPPED: %s (already exists)\n", completed, count, result.filename)
		} else {
			atomic.AddInt64(&succeeded, 1)
			fmt.Printf("[%d/%d] OK: %s\n", completed, count, result.filename)
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\nCompleted in %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Succeeded: %d\n", succeeded)
	if skipped > 0 {
		fmt.Printf("  Skipped:   %d (already exist in output directory)\n", skipped)
	}
	if failed > 0 {
		fmt.Printf("  Failed:    %d\n", failed)

		// Write failed UUIDs to ~/.darwin-photos/ with timestamp
		failedFile, err := writeFailedUUIDs(failedUUIDs)
		if err != nil {
			fmt.Printf("  Warning: could not write failed UUIDs to file: %v\n", err)
		} else {
			fmt.Printf("\nFailed UUIDs written to: %s\n", failedFile)
			fmt.Printf("Retry with: darwin-photos export --from-file %s %s\n", failedFile, outputDir)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d exports failed", failed)
	}
	return nil
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
			results <- exportResult{uuid: job.uuid, filename: filename, skipped: true}
			continue
		}

		// Download with retry
		if err := client.DownloadPhotoWithRetry(asset.DownloadURL, outputPath, config.maxRetries); err != nil {
			results <- exportResult{uuid: job.uuid, err: err}
			continue
		}

		results <- exportResult{uuid: job.uuid, filename: filename}
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
