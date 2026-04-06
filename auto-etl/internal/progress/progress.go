package progress

import (
	"fmt"
	"os"
	"strings"
)

// Bar renders a progress bar to stderr with a label and numeric indicator.
// It overwrites the current line on each update using \r.
type Bar struct {
	Label string
	Total int
	Width int // bar width in characters (default 30)
}

// Update prints the current progress state to stderr.
func (b *Bar) Update(current int) {
	width := b.Width
	if width == 0 {
		width = 30
	}
	total := b.Total
	if total <= 0 {
		total = 1
	}

	fraction := float64(current) / float64(total)
	if fraction > 1 {
		fraction = 1
	}

	filled := int(fraction * float64(width))
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	fmt.Fprintf(os.Stderr, "\r%-12s [%s] %d/%d", b.Label, bar, current, total)
}

// Done finishes the progress bar by printing a newline.
func (b *Bar) Done() {
	b.Update(b.Total)
	fmt.Fprintln(os.Stderr)
}
