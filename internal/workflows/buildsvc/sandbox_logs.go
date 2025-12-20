package buildsvc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func startSandboxLogStreamer(ctx context.Context, path string, out io.Writer, observer func(string)) (func(), error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sandbox log path is empty")
	}
	localCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		if err := streamSandboxFile(localCtx, path, out, observer); err != nil && out != nil {
			fmt.Fprintf(out, "[sandbox] log stream error: %v\n", err)
		}
	}()

	return func() {
		cancel()
		<-done
	}, nil
}

func streamSandboxFile(ctx context.Context, path string, out io.Writer, observer func(string)) error {
	file, err := waitForSandboxLog(ctx, path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			emitSandboxLine(strings.TrimRight(line, "\r\n"), out, observer)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
					continue
				}
			}
			return err
		}
	}
}

func waitForSandboxLog(ctx context.Context, path string) (*os.File, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		file, err := os.Open(path)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func emitSandboxLine(line string, out io.Writer, observer func(string)) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if out != nil {
		fmt.Fprintf(out, "[sandbox] %s\n", line)
	}
	if observer != nil {
		observer(line)
	}
}
