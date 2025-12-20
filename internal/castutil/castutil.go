// File: internal/castutil/castutil.go
// Brief: Internal castutil package implementation for 'castutil'.

// Package castutil provides castutil helpers.

package castutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/example/ktl/internal/caststream"
	"github.com/go-logr/logr"
)

// StartCastServer boots a caststream server and surfaces early failures to the caller.
func StartCastServer(ctx context.Context, srv *caststream.Server, label string, logger logr.Logger, errOut io.Writer) error {
	if srv == nil {
		return nil
	}
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			if errOut != nil {
				fmt.Fprintf(errOut, "%s failed: %v\n", label, err)
			}
			return err
		}
	case <-time.After(250 * time.Millisecond):
		// Server is running; monitor for later failures.
		go func() {
			for err := range errCh {
				if err == nil {
					continue
				}
				if logger.GetSink() != nil {
					logger.Error(err, fmt.Sprintf("%s exited", label))
				}
				if errOut != nil {
					fmt.Fprintf(errOut, "%s exited: %v\n", label, err)
				}
			}
		}()
	}

	return nil
}
