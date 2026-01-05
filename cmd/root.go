package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	libraryPath string
)

var rootCmd = &cobra.Command{
	Use:   "photoscli",
	Short: "CLI tool for exploring your iCloud Photos library",
	Long: `photoscli reads your local iCloud Photos database to list photos,
show detailed metadata, and download cloud-only photos.

It works by reading the Photos.sqlite database directly, which means
you don't need to re-authenticate with iCloud.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Default library path
	homeDir, _ := os.UserHomeDir()
	defaultLibrary := filepath.Join(homeDir, "Pictures", "Photos Library.photoslibrary")

	rootCmd.PersistentFlags().StringVarP(&libraryPath, "library", "l", defaultLibrary,
		"Path to Photos library")
}

func getLibraryPath() string {
	return libraryPath
}
