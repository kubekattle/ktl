// File: cmd/ktl/confirm.go
// Brief: Shared confirmation prompts for destructive commands.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

type confirmMode string

const (
	confirmModeYes   confirmMode = "yes"
	confirmModeExact confirmMode = "exact"
)

func confirmAction(in io.Reader, out io.Writer, interactive bool, prompt string, mode confirmMode, expected string) error {
	if !interactive {
		return errors.New("refusing to proceed without a TTY; rerun with --auto-approve")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "Confirm:"
	}
	fmt.Fprint(out, prompt+" ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	reply := strings.TrimSpace(line)
	switch mode {
	case confirmModeYes:
		if reply != "yes" {
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
