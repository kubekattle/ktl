// File: cmd/ktl/capture_analyze.go
// Brief: CLI command wiring and implementation for 'capture analyze'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/spf13/cobra"
)

func newCaptureAnalyzeCommand() *cobra.Command {
	var uiAddr string
	var openBrowser bool

	cmd := &cobra.Command{
		Use:   "analyze <CAPTURE_ARTIFACT>",
		Short: "Open a capture artifact in the SQLite-backed viewer UI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uiAddr = strings.TrimSpace(uiAddr)
			if uiAddr == "" {
				uiAddr = ":8080"
			}

			level, _ := cmd.Flags().GetString("log-level")
			logger, err := buildLogger(level)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			artifact := args[0]
			id := randomHexID()
			clusterInfo := fmt.Sprintf("Capture analyze · %s", filepath.Base(artifact))

			uiServer := caststream.New(uiAddr, caststream.ModeWeb, clusterInfo, logger.WithName("capture-analyze"))
			if err := uiServer.ImportCapture(ctx, id, artifact); err != nil {
				return err
			}
			if err := castutil.StartCastServer(ctx, uiServer, "ktl capture analyze UI", logger.WithName("capture-analyze"), cmd.ErrOrStderr()); err != nil {
				return err
			}
			url, note := captureAnalyzeURL(uiAddr, id)
			fmt.Fprintf(cmd.ErrOrStderr(), "Serving capture analyze UI on %s\n", uiAddr)
			fmt.Fprintf(cmd.ErrOrStderr(), "Open: %s\n", url)
			if note != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), note)
			}
			if openBrowser {
				if err := openURL(url); err != nil {
					return err
				}
			}
			<-ctx.Done()
			return nil
		},
	}

	cmd.Flags().StringVar(&uiAddr, "ui", ":8080", "Serve the capture viewer at this address (e.g. :8080)")
	cmd.Flags().BoolVar(&openBrowser, "open", false, "Open the viewer URL in the default browser")
	return cmd
}

func randomHexID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func captureAnalyzeURL(addr string, id string) (string, string) {
	a := strings.TrimSpace(addr)
	if a == "" {
		a = ":8080"
	}
	host := ""
	port := ""
	if strings.HasPrefix(a, ":") {
		port = strings.TrimPrefix(a, ":")
	} else {
		h, p, err := net.SplitHostPort(a)
		if err == nil {
			host = strings.TrimSpace(h)
			port = strings.TrimSpace(p)
		}
	}
	if port == "" {
		port = "8080"
	}

	openHost := host
	note := ""
	switch openHost {
	case "":
		openHost = "localhost"
	case "127.0.0.1", "localhost":
		// Preserve explicit loopback host in the output; don't guess.
	case "0.0.0.0", "::", "[::]":
		openHost = "127.0.0.1"
		note = "Note: the viewer is listening on all interfaces; if accessing remotely, use your host IP instead of 127.0.0.1."
	}
	return fmt.Sprintf("http://%s:%s/capture/view/%s", openHost, port, id), note
}

func openURL(url string) error {
	u := strings.TrimSpace(url)
	if u == "" {
		return fmt.Errorf("url is required")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		return fmt.Errorf("--open is not supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}
