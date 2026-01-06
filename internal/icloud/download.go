package icloud

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// DownloadPhoto downloads a photo from the given URL to the output path.
func (c *Client) DownloadPhoto(downloadURL, outputPath string) error {
	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Download URLs from iCloud are pre-signed and don't need auth headers
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	// Stream to file
	written, err := io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(outputPath) // Clean up partial file
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Verify size if we know it
	if resp.ContentLength > 0 && written != resp.ContentLength {
		os.Remove(outputPath)
		return fmt.Errorf("incomplete download: got %d bytes, expected %d", written, resp.ContentLength)
	}

	return nil
}

// DownloadPhotoWithProgress downloads a photo with progress callback.
func (c *Client) DownloadPhotoWithProgress(downloadURL, outputPath string, progress func(downloaded, total int64)) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	// Wrap reader with progress tracking
	reader := &progressReader{
		reader:   resp.Body,
		total:    resp.ContentLength,
		progress: progress,
	}

	written, err := io.Copy(file, reader)
	if err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	if resp.ContentLength > 0 && written != resp.ContentLength {
		os.Remove(outputPath)
		return fmt.Errorf("incomplete download: got %d bytes, expected %d", written, resp.ContentLength)
	}

	return nil
}

// progressReader wraps an io.Reader to report progress.
type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	progress   func(downloaded, total int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.downloaded += int64(n)
	if r.progress != nil {
		r.progress(r.downloaded, r.total)
	}
	return n, err
}
