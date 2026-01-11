package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/spf13/cobra"
)

var statsJSON bool

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show library statistics",
	Long:  `Show total count and size of photos and videos in your iCloud Photos library.`,
	RunE:  runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)

	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "Output as JSON")
}

func runStats(cmd *cobra.Command, args []string) error {
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	stats, err := db.GetStats(photosDB.DB())
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	if statsJSON {
		return outputStatsJSON(stats)
	}

	return outputStatsPlain(stats)
}

func outputStatsJSON(stats *db.Stats) error {
	// Calculate iCloud storage estimate
	iCloudStorage := stats.TotalSize - stats.LocalCacheSize
	cloudDerivatives := iCloudStorage - stats.OriginalSize

	output := struct {
		TotalCount       int   `json:"total_count"`
		TotalSize        int64 `json:"total_size"`
		ICloudStorage    int64 `json:"icloud_storage"`
		OriginalSize     int64 `json:"original_size"`
		CloudDerivatives int64 `json:"cloud_derivatives"`
		LocalCacheSize   int64 `json:"local_cache_size"`
		DerivativeSize   int64 `json:"derivative_size"`
		LocalCount       int   `json:"local_count"`
		LocalSize        int64 `json:"local_size"`
		CloudCount       int   `json:"cloud_count"`
		CloudSize        int64 `json:"cloud_size"`
		PhotoCount       int   `json:"photo_count"`
		PhotoSize        int64 `json:"photo_size"`
		VideoCount       int   `json:"video_count"`
		VideoSize        int64 `json:"video_size"`
	}{
		TotalCount:       stats.TotalCount,
		TotalSize:        stats.TotalSize,
		ICloudStorage:    iCloudStorage,
		OriginalSize:     stats.OriginalSize,
		CloudDerivatives: cloudDerivatives,
		LocalCacheSize:   stats.LocalCacheSize,
		DerivativeSize:   stats.DerivativeSize,
		LocalCount:       stats.LocalCount,
		LocalSize:        stats.LocalSize,
		CloudCount:       stats.CloudCount,
		CloudSize:        stats.CloudSize,
		PhotoCount:       stats.PhotoCount,
		PhotoSize:        stats.PhotoSize,
		VideoCount:       stats.VideoCount,
		VideoSize:        stats.VideoSize,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputStatsPlain(stats *db.Stats) error {
	// Calculate iCloud storage estimate (total minus local cache)
	iCloudStorage := stats.TotalSize - stats.LocalCacheSize
	cloudDerivatives := iCloudStorage - stats.OriginalSize

	fmt.Printf("%-45s %10s\n", "iCloud Storage (counts toward quota):", formatBytes(iCloudStorage))
	fmt.Printf("  %-43s %10s\n", "Originals:", formatBytes(stats.OriginalSize))
	fmt.Printf("  %-43s %10s\n", "Cloud derivatives:", formatBytes(cloudDerivatives))
	fmt.Println()
	fmt.Printf("%-45s %10s\n", "Local Cache Only (not in iCloud):", formatBytes(stats.LocalCacheSize))
	fmt.Printf("  %-43s %10s\n", "JPEG previews for cloud photos", formatBytes(stats.LocalCacheSize))
	fmt.Println()
	fmt.Printf("%-45s %10s\n", "Total on this Mac:", formatBytes(stats.TotalSize))
	fmt.Println()
	fmt.Printf("By type:\n")
	fmt.Printf("  Photos: %6d items  %10s\n", stats.PhotoCount, formatBytes(stats.PhotoSize))
	fmt.Printf("  Videos: %6d items  %10s\n", stats.VideoCount, formatBytes(stats.VideoSize))
	fmt.Println()
	fmt.Printf("By availability:\n")
	fmt.Printf("  Downloaded:  %6d items  %10s\n", stats.LocalCount, formatBytes(stats.LocalSize))
	fmt.Printf("  Cloud-only:  %6d items  %10s\n", stats.CloudCount, formatBytes(stats.CloudSize))
	return nil
}
