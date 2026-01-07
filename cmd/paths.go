package cmd

import (
	"path/filepath"

	"github.com/cleanexit0/darwin-photos/internal/models"
)

// buildLocalPath returns the full path to an asset's original file in the Photos library.
func buildLocalPath(asset *models.Asset) string {
	libraryPath := getLibraryPath()
	dir := asset.Directory
	if dir == "" && len(asset.UUID) > 0 {
		dir = string(asset.UUID[0])
	}
	return filepath.Join(libraryPath, "originals", dir, asset.Filename)
}
