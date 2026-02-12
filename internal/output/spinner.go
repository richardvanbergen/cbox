package output

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// spinnerFrames are the characters cycled through for the spinner animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// LineSpinner manages a set of lines where some have a spinning indicator
// that updates in-place until resolved.
type LineSpinner struct {
	mu    sync.Mutex
	w     io.Writer
	lines []spinnerLine
	done  chan struct{}
	frame int
}

type spinnerLine struct {
	text     string // the full line text (with %s placeholder for status)
	status   string // resolved status text, empty while spinning
	resolved bool
}

// NewLineSpinner creates a spinner that writes to stdout.
func NewLineSpinner(count int) *LineSpinner {
	return &LineSpinner{
		w:     os.Stdout,
		lines: make([]spinnerLine, count),
		done:  make(chan struct{}),
	}
}

// SetLine sets the format text for a line. The text should contain one %s
// placeholder where the status (spinner or resolved value) will be inserted.
func (s *LineSpinner) SetLine(index int, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines[index].text = text
}

// Resolve replaces the spinner on the given line with a final status string.
func (s *LineSpinner) Resolve(index int, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines[index].status = status
	s.lines[index].resolved = true

	// Check if all lines are resolved
	allDone := true
	for _, l := range s.lines {
		if !l.resolved {
			allDone = false
			break
		}
	}
	if allDone {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	}
}

// Run starts the spinner animation. It prints all lines initially, then
// updates them in-place at ~80ms intervals. It blocks until all lines are
// resolved or the returned stop function is called.
func (s *LineSpinner) Run() {
	s.mu.Lock()
	// Print all lines initially
	for _, l := range s.lines {
		status := progressPrefix.Render(spinnerFrames[0])
		if l.resolved {
			status = l.status
		}
		fmt.Fprintf(s.w, "%s\n", fmt.Sprintf(l.text, status))
	}
	s.mu.Unlock()

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			s.redraw()
			return
		case <-ticker.C:
			s.frame++
			s.redraw()
		}
	}
}

// Spin displays a spinner animation alongside msg while fn executes.
// On success the spinner line is replaced with "✓ <msg>".
// On error it is replaced with "› <msg>" so subsequent error output
// reads naturally.
//
// Example:
//
//	err := output.Spin("Starting sandbox", func() error {
//	    return sandbox.Up(...)
//	})
func Spin(msg string, fn func() error) error {
	return spinTo(os.Stdout, msg, fn)
}

// spinTo is the testable core of Spin, accepting an explicit writer.
func spinTo(w io.Writer, msg string, fn func() error) error {
	ch := make(chan error, 1)
	go func() {
		ch <- fn()
	}()

	frame := 0
	fmt.Fprintf(w, "%s %s", progressPrefix.Render(spinnerFrames[frame]), msg)

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-ch:
			fmt.Fprintf(w, "\r\033[2K")
			if err != nil {
				fmt.Fprintf(w, "%s %s\n", progressPrefix.Render("›"), msg)
			} else {
				fmt.Fprintf(w, "%s %s\n", successPrefix.Render("✓"), msg)
			}
			return err
		case <-ticker.C:
			frame++
			char := spinnerFrames[frame%len(spinnerFrames)]
			fmt.Fprintf(w, "\r\033[2K%s %s", progressPrefix.Render(char), msg)
		}
	}
}

// redraw moves the cursor up and reprints all lines.
func (s *LineSpinner) redraw() {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.lines)
	// Move cursor up n lines
	fmt.Fprintf(s.w, "\033[%dA", n)

	frameChar := spinnerFrames[s.frame%len(spinnerFrames)]
	for _, l := range s.lines {
		status := progressPrefix.Render(frameChar)
		if l.resolved {
			status = l.status
		}
		// Clear line and print
		fmt.Fprintf(s.w, "\033[2K%s\n", fmt.Sprintf(l.text, status))
	}
}
