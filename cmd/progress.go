package cmd

import (
	"fmt"
	"strings"
	"time"
)

// ProgressBar displays a simple progress bar in the terminal.
// It updates in-place using carriage return.
// Supports both count-based and size-based progress tracking.
type ProgressBar struct {
	total     int
	completed int
	width     int
	startTime time.Time

	// Size-based tracking (optional)
	totalBytes     int64
	completedBytes int64
	sizeMode       bool
}

// NewProgressBar creates a new progress bar with the given total count.
func NewProgressBar(total int) *ProgressBar {
	return &ProgressBar{
		total:     total,
		width:     30,
		startTime: time.Now(),
	}
}

// NewSizeProgressBar creates a progress bar that tracks bytes instead of count.
func NewSizeProgressBar(totalCount int, totalBytes int64) *ProgressBar {
	return &ProgressBar{
		total:      totalCount,
		totalBytes: totalBytes,
		width:      30,
		startTime:  time.Now(),
		sizeMode:   true,
	}
}

// Add increments the progress by n items and redraws the bar.
func (p *ProgressBar) Add(n int) {
	p.completed += n
	p.draw()
}

// AddBytes increments the progress by n bytes (for size-based tracking).
func (p *ProgressBar) AddBytes(count int, bytes int64) {
	p.completed += count
	p.completedBytes += bytes
	p.draw()
}

// draw renders the progress bar to stdout.
func (p *ProgressBar) draw() {
	if p.total == 0 {
		return
	}

	var pct float64
	var stats string

	if p.sizeMode && p.totalBytes > 0 {
		// Size-based progress with count in parentheses
		pct = float64(p.completedBytes) / float64(p.totalBytes)
		stats = fmt.Sprintf("%s/%s (%d/%d)", formatBytes(p.completedBytes), formatBytes(p.totalBytes), p.completed, p.total)
	} else {
		// Count-based progress
		pct = float64(p.completed) / float64(p.total)
		stats = fmt.Sprintf("%d/%d", p.completed, p.total)
	}

	filled := int(pct * float64(p.width))
	if filled > p.width {
		filled = p.width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", p.width-filled)

	// Calculate ETA and throughput
	elapsed := time.Since(p.startTime)
	extra := ""
	if p.sizeMode && p.completedBytes > 0 {
		// Show throughput for size-based progress
		throughput := float64(p.completedBytes) / elapsed.Seconds()
		extra = fmt.Sprintf(" %s/s", formatBytes(int64(throughput)))
		if pct < 1.0 && p.totalBytes > p.completedBytes {
			remaining := time.Duration(float64(elapsed) / float64(p.completedBytes) * float64(p.totalBytes-p.completedBytes))
			extra += fmt.Sprintf(" ETA %s", formatDuration(remaining))
		}
	} else if p.completed > 0 && p.completed < p.total {
		remaining := time.Duration(float64(elapsed) / float64(p.completed) * float64(p.total-p.completed))
		extra = fmt.Sprintf(" ETA %s", formatDuration(remaining))
	}

	// Pad with spaces to overwrite previous longer output
	output := fmt.Sprintf("\r[%s] %s (%.0f%%)%s", bar, stats, pct*100, extra)
	fmt.Printf("%-100s", output)
}

// Finish completes the progress bar and moves to a new line.
func (p *ProgressBar) Finish() {
	p.draw()
	fmt.Println()
}

// Clear removes the progress bar line.
func (p *ProgressBar) Clear() {
	fmt.Printf("\r%s\r", strings.Repeat(" ", p.width+40))
}

// formatDuration formats a duration in a human-readable short form.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// formatBytes formats bytes in human-readable form.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
