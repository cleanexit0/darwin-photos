package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"github.com/cleanexit0/darwin-photos/internal/db"
)

var (
	lsLimit      int
	lsOffset     int
	lsLocalOnly  bool
	lsCloudOnly  bool
	lsPhotosOnly bool
	lsVideosOnly bool
	lsSort       string
	lsDesc       bool
	lsTrashed    bool
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List photos in the library",
	Long: `List photos and videos in your iCloud Photos library.

Shows UUID, filename, type, size, date, and download status (Local/Cloud).`,
	RunE: runLs,
}

func init() {
	rootCmd.AddCommand(lsCmd)

	lsCmd.Flags().IntVarP(&lsLimit, "limit", "n", 50, "Limit number of results")
	lsCmd.Flags().IntVarP(&lsOffset, "offset", "o", 0, "Offset for pagination")
	lsCmd.Flags().BoolVar(&lsLocalOnly, "local", false, "Show only locally available photos")
	lsCmd.Flags().BoolVar(&lsCloudOnly, "cloud", false, "Show only cloud-only photos")
	lsCmd.Flags().BoolVar(&lsPhotosOnly, "photos", false, "Show only photos (no videos)")
	lsCmd.Flags().BoolVar(&lsVideosOnly, "videos", false, "Show only videos")
	lsCmd.Flags().StringVarP(&lsSort, "sort", "s", "date", "Sort by: date, name, size")
	lsCmd.Flags().BoolVar(&lsDesc, "desc", true, "Sort descending (newest first)")
	lsCmd.Flags().BoolVar(&lsTrashed, "trashed", false, "Include trashed items")
}

func runLs(cmd *cobra.Command, args []string) error {
	// Open database
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	// Build query options
	opts := db.ListOptions{
		Limit:          lsLimit,
		Offset:         lsOffset,
		LocalOnly:      lsLocalOnly,
		CloudOnly:      lsCloudOnly,
		PhotosOnly:     lsPhotosOnly,
		VideosOnly:     lsVideosOnly,
		IncludeTrashed: lsTrashed,
		SortBy:         lsSort,
		SortDescending: lsDesc,
	}

	// Query assets
	assets, total, err := db.ListAssets(photosDB.DB(), opts)
	if err != nil {
		return fmt.Errorf("failed to list assets: %w", err)
	}

	// Get stats
	_, localCount, cloudCount, _ := db.GetStats(photosDB.DB())

	// Create table using standard library tabwriter
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Header
	fmt.Fprintln(w, "UUID\tFilename\tType\tSize\tDate\tStatus")

	for _, a := range assets {
		// Truncate UUID for display
		uuid := a.UUID
		if len(uuid) > 8 {
			uuid = uuid[:8]
		}

		// Format filename (use original if available)
		filename := a.OriginalFilename
		if filename == "" {
			filename = a.Filename
		}
		if len(filename) > 30 {
			filename = filename[:27] + "..."
		}

		// Format date
		date := ""
		if !a.DateCreated.IsZero() {
			date = a.DateCreated.Format("2006-01-02")
		}

		// Format size
		size := humanize.Bytes(uint64(a.FileSize))

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			uuid,
			filename,
			a.TypeString(),
			size,
			date,
			a.StatusString(),
		)
	}

	w.Flush()

	// Print summary
	fmt.Printf("\nShowing %d of %d assets (Local: %d | Cloud: %d)\n",
		len(assets), total, localCount, cloudCount)

	return nil
}
