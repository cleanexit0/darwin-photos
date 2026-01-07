package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// readUUIDsFromFile reads UUIDs from a file, one per line.
// Empty lines and lines starting with # are ignored.
func readUUIDsFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return parseUUIDs(bufio.NewScanner(file))
}

// readUUIDsFromStdin reads UUIDs from stdin, one per line.
func readUUIDsFromStdin() ([]string, error) {
	return parseUUIDs(bufio.NewScanner(os.Stdin))
}

// parseUUIDs parses UUIDs from a scanner, one per line.
// Empty lines and lines starting with # are ignored.
func parseUUIDs(scanner *bufio.Scanner) ([]string, error) {
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
