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
	output := struct {
		TotalCount int   `json:"total_count"`
		TotalSize  int64 `json:"total_size"`
		LocalCount int   `json:"local_count"`
		LocalSize  int64 `json:"local_size"`
		CloudCount int   `json:"cloud_count"`
		CloudSize  int64 `json:"cloud_size"`
		PhotoCount int   `json:"photo_count"`
		PhotoSize  int64 `json:"photo_size"`
		VideoCount int   `json:"video_count"`
		VideoSize  int64 `json:"video_size"`
	}{
		TotalCount: stats.TotalCount,
		TotalSize:  stats.TotalSize,
		LocalCount: stats.LocalCount,
		LocalSize:  stats.LocalSize,
		CloudCount: stats.CloudCount,
		CloudSize:  stats.CloudSize,
		PhotoCount: stats.PhotoCount,
		PhotoSize:  stats.PhotoSize,
		VideoCount: stats.VideoCount,
		VideoSize:  stats.VideoSize,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputStatsPlain(stats *db.Stats) error {
	fmt.Printf("Total:  %6d items  %10s\n", stats.TotalCount, formatBytes(stats.TotalSize))
	fmt.Printf("  Local:  %6d items  %10s\n", stats.LocalCount, formatBytes(stats.LocalSize))
	fmt.Printf("  Cloud:  %6d items  %10s\n", stats.CloudCount, formatBytes(stats.CloudSize))
	fmt.Printf("Photos: %6d items  %10s\n", stats.PhotoCount, formatBytes(stats.PhotoSize))
	fmt.Printf("Videos: %6d items  %10s\n", stats.VideoCount, formatBytes(stats.VideoSize))
	return nil
}
