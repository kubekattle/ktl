// File: cmd/ktl/confirm.go
// Brief: Shared confirmation prompts for destructive commands.

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type confirmMode string

const (
	confirmModeYes   confirmMode = "yes"
	confirmModeExact confirmMode = "exact"
)

func confirmAction(ctx context.Context, in io.Reader, out io.Writer, dec approvalDecision, prompt string, mode confirmMode, expected string) error {
	if out == nil {
		return errors.New("confirmation output is nil")
	}
	// Never prompt if already approved.
	if dec.Approved {
		return nil
	}
	if dec.NonInteractive || !dec.InteractiveTTY {
		return errors.New("refusing to proceed without confirmation; rerun with --yes")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "Confirm:"
	}
	fmt.Fprint(out, prompt+" ")

	closeInputOnCancel := func() {
		rc, ok := in.(io.ReadCloser)
		if !ok {
			return
		}
		// Never close the real process stdin; it can break subsequent prompts and shell sessions.
		if f, ok := in.(*os.File); ok && os.Stdin != nil && f.Fd() == os.Stdin.Fd() {
			return
		}
		_ = rc.Close()
	}

	reader := bufio.NewReader(in)
	readResult := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := reader.ReadString('\n')
		readResult <- struct {
			line string
			err  error
		}{line: line, err: err}
	}()

	var line string
	var err error
	select {
	case <-ctx.Done():
		closeInputOnCancel()
		fmt.Fprintln(out)
		return ctx.Err()
	case res := <-readResult:
		line, err = res.line, res.err
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	reply := strings.TrimSpace(line)
	switch mode {
	case confirmModeYes:
		if !strings.EqualFold(reply, "yes") {
			return errors.New("aborted")
		}
		return nil
	case confirmModeExact:
		if strings.TrimSpace(expected) == "" {
			return errors.New("confirmation token missing")
		}
		if reply != expected {
			return errors.New("aborted")
		}
		return nil
	default:
		return fmt.Errorf("unknown confirmation mode: %s", mode)
	}
}
