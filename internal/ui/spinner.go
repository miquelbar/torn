package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Spinner displays a braille animation on a terminal output stream.
type Spinner struct {
	w       io.Writer
	message string
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	running bool
	force   bool
}

// NewSpinner creates a new Spinner writing to w. If w is nil, defaults to os.Stderr.
func NewSpinner(w io.Writer, message string) *Spinner {
	if w == nil {
		w = os.Stderr
	}
	return &Spinner{
		w:       w,
		message: message,
	}
}

// SetForceTTY enables the spinner animation even if output is not detected as an interactive terminal.
func (s *Spinner) SetForceTTY(force bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.force = force
}

// Start begins the spinner animation in a background goroutine.
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}
	if !s.force && !isTerminal(s.w) {
		return
	}

	s.running = true
	stop := make(chan struct{})
	done := make(chan struct{})
	s.stop = stop
	s.done = done

	go func() {
		defer close(done)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				s.mu.Lock()
				fmt.Fprint(s.w, "\r\033[K")
				s.mu.Unlock()
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.running {
					fmt.Fprintf(s.w, "\r\033[36m%s\033[0m %s...", frames[i], s.message)
				}
				s.mu.Unlock()
				i = (i + 1) % len(frames)
			}
		}
	}()
}

// Stop stops the spinner and clears the current line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	stop := s.stop
	done := s.done
	close(stop)
	s.mu.Unlock()

	<-done
}

// isTerminal checks if w is a character device terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
