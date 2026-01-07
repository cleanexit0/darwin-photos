package cmd

import (
	"fmt"
	"strings"
	"time"
)

// ProgressBar displays a simple progress bar in the terminal.
// It updates in-place using carriage return.
type ProgressBar struct {
	total     int
	completed int
	width     int
	startTime time.Time
}

// NewProgressBar creates a new progress bar with the given total count.
func NewProgressBar(total int) *ProgressBar {
	return &ProgressBar{
		total:     total,
		width:     30,
		startTime: time.Now(),
	}
}

// Add increments the progress by n and redraws the bar.
func (p *ProgressBar) Add(n int) {
	p.completed += n
	p.draw()
}

// draw renders the progress bar to stdout.
func (p *ProgressBar) draw() {
	if p.total == 0 {
		return
	}

	pct := float64(p.completed) / float64(p.total)
	filled := int(pct * float64(p.width))
	if filled > p.width {
		filled = p.width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", p.width-filled)

	// Calculate ETA
	elapsed := time.Since(p.startTime)
	eta := ""
	if p.completed > 0 && p.completed < p.total {
		remaining := time.Duration(float64(elapsed) / float64(p.completed) * float64(p.total-p.completed))
		eta = fmt.Sprintf(" ETA %s", formatDuration(remaining))
	}

	// Pad with spaces to overwrite previous longer output
	output := fmt.Sprintf("\r[%s] %d/%d (%.0f%%)%s", bar, p.completed, p.total, pct*100, eta)
	fmt.Printf("%-70s", output)
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
