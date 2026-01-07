package icloud

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DownloadPhoto downloads a photo from the given URL to the output path.
func (c *Client) DownloadPhoto(downloadURL, outputPath string) error {
	return c.DownloadPhotoWithRetry(downloadURL, outputPath, 0)
}

// DownloadPhotoWithRetry downloads a photo with configurable retry attempts.
// maxRetries of 0 means no retries (single attempt).
func (c *Client) DownloadPhotoWithRetry(downloadURL, outputPath string, maxRetries int) error {
	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s...
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			time.Sleep(backoff)
		}

		err := c.downloadOnce(downloadURL, outputPath)
		if err == nil {
			return nil
		}
		lastErr = err

		// Don't retry on non-retryable errors
		if !isRetryableError(err) {
			return err
		}
	}

	if maxRetries > 0 {
		return fmt.Errorf("%w (after %d retries)", lastErr, maxRetries)
	}
	return lastErr
}

// isRetryableError returns true for transient errors that may succeed on retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Retry on network errors, EOF, timeouts, and 5xx status codes
	return errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) ||
		contains(errStr, "unexpected EOF") ||
		contains(errStr, "connection reset") ||
		contains(errStr, "connection refused") ||
		contains(errStr, "timeout") ||
		contains(errStr, "status 5") ||
		contains(errStr, "incomplete download")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (c *Client) downloadOnce(downloadURL, outputPath string) error {
	// Download URLs from iCloud are pre-signed and don't need auth headers
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("download failed with status %d (server error)", resp.StatusCode)
	}
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
