package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/models"
	"github.com/spf13/cobra"
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
	lsJSON       bool
	lsStart      string
	lsEnd        string
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
	lsCmd.Flags().BoolVar(&lsJSON, "json", false, "Output as JSON")
	lsCmd.Flags().StringVar(&lsStart, "start", "", "Start date (YYYY-MM-DD), inclusive")
	lsCmd.Flags().StringVar(&lsEnd, "end", "", "End date (YYYY-MM-DD), inclusive")
}

func runLs(cmd *cobra.Command, args []string) error {
	// Parse date filters
	var startDate, endDate time.Time
	var err error
	if lsStart != "" {
		startDate, err = time.Parse("2006-01-02", lsStart)
		if err != nil {
			return fmt.Errorf("invalid start date %q: use YYYY-MM-DD format", lsStart)
		}
	}
	if lsEnd != "" {
		endDate, err = time.Parse("2006-01-02", lsEnd)
		if err != nil {
			return fmt.Errorf("invalid end date %q: use YYYY-MM-DD format", lsEnd)
		}
	}

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
		StartDate:      startDate,
		EndDate:        endDate,
	}

	// Query assets
	assets, total, err := db.ListAssets(photosDB.DB(), opts)
	if err != nil {
		return fmt.Errorf("failed to list assets: %w", err)
	}

	if lsJSON {
		return outputLsJSON(assets, total)
	}

	return outputLsPlain(assets, total)
}

func outputLsJSON(assets []*models.Asset, total int) error {
	type jsonAsset struct {
		UUID             string  `json:"uuid"`
		Filename         string  `json:"filename"`
		OriginalFilename string  `json:"original_filename,omitempty"`
		Type             string  `json:"type"`
		Size             int64   `json:"size"`
		Date             string  `json:"date,omitempty"`
		Status           string  `json:"status"`
		Width            int     `json:"width,omitempty"`
		Height           int     `json:"height,omitempty"`
		Duration         float64 `json:"duration,omitempty"`
	}

	output := struct {
		Assets []*jsonAsset `json:"assets"`
		Count  int          `json:"count"`
		Total  int          `json:"total"`
	}{
		Assets: make([]*jsonAsset, 0, len(assets)),
		Count:  len(assets),
		Total:  total,
	}

	for _, a := range assets {
		ja := &jsonAsset{
			UUID:     a.UUID,
			Filename: a.Filename,
			Type:     a.TypeString(),
			Size:     a.FileSize,
			Status:   a.StatusString(),
		}
		if a.OriginalFilename != "" && a.OriginalFilename != a.Filename {
			ja.OriginalFilename = a.OriginalFilename
		}
		if !a.DateCreated.IsZero() {
			ja.Date = a.DateCreated.Format("2006-01-02T15:04:05Z")
		}
		if a.Width > 0 {
			ja.Width = a.Width
		}
		if a.Height > 0 {
			ja.Height = a.Height
		}
		if a.Duration > 0 {
			ja.Duration = a.Duration
		}
		output.Assets = append(output.Assets, ja)
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputLsPlain(assets []*models.Asset, total int) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	for _, a := range assets {
		filename := a.OriginalFilename
		if filename == "" {
			filename = a.Filename
		}
		if len(filename) > 40 {
			filename = filename[:37] + "..."
		}

		date := "-"
		if !a.DateCreated.IsZero() {
			date = a.DateCreated.Format("Jan _2 15:04")
		}

		size := formatBytes(a.FileSize)

		// Status: L=local, C=cloud-only
		status := "L"
		if !a.IsLocallyAvailable() {
			status = "C"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			status,
			a.UUID,
			size,
			date,
			a.TypeString(),
			filename,
		)
	}

	w.Flush()

	if len(assets) < total {
		fmt.Fprintf(os.Stderr, "# %d/%d shown\n", len(assets), total)
	}

	return nil
}
