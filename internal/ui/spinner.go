// spinner.go implements the CLI spinner displayed while ktl performs background work (captures, remotes, etc.).
package ui

import (
	"fmt"
	"io"
	"time"
)

// StartSpinner prints a lightweight ASCII spinner until the returned
// stop function is called. The stop function prints either "[done]"
// or "[fail]" depending on the success flag.
func StartSpinner(w io.Writer, message string) func(success bool) {
	frames := []rune{'|', '/', '-', '\\'}
	done := make(chan struct{})
	go func() {
		defer fmt.Fprintf(w, "\r%s    \r", message)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		idx := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Fprintf(w, "\r%s %c", message, frames[idx])
				idx = (idx + 1) % len(frames)
			}
		}
	}()
	return func(success bool) {
		select {
		case <-done:
		default:
			close(done)
		}
		status := "[done]"
		if !success {
			status = "[fail]"
		}
		fmt.Fprintf(w, "\r%s %s\n", message, status)
	}
}
