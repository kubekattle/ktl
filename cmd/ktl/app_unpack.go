// app_unpack.go provides 'ktl app unpack' so teams can extract manifests, templates, and attachments from archived snapshots.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/apparchive"
	"github.com/spf13/cobra"
)

func newAppUnpackCommand() *cobra.Command {
	var (
		archivePath   string
		snapshotName  string
		outputDir     string
		includeAttach bool
	)
	outputDir = "./app-unpack"

	cmd := &cobra.Command{
		Use:   "unpack",
		Short: "Extract manifests (and optional attachments) from an app archive snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			reader, err := apparchive.NewReader(archivePath)
			if err != nil {
				return err
			}
			defer reader.Close()

			snapshot, err := reader.ResolveSnapshot(snapshotName)
			if err != nil {
				return err
			}
			manifests, err := reader.ReadManifests(snapshot)
			if err != nil {
				return err
			}
			if len(manifests) == 0 {
				return fmt.Errorf("snapshot %s has no manifests", snapshot.Name)
			}
			manifestDir := filepath.Join(outputDir, "manifests")
			if err := os.MkdirAll(manifestDir, 0o755); err != nil {
				return fmt.Errorf("ensure manifest dir: %w", err)
			}
			for idx, manifest := range manifests {
				filename := fmt.Sprintf("%03d_%s.yaml", idx+1, sanitizeName(manifest.Kind+"-"+manifest.Namespace+"-"+manifest.Name))
				path := filepath.Join(manifestDir, filename)
				if err := os.WriteFile(path, []byte(manifest.Body), 0o644); err != nil {
					return fmt.Errorf("write manifest %s: %w", manifest.Name, err)
				}
			}

			if includeAttach {
				attachments, err := reader.ReadAttachments(snapshot)
				if err != nil {
					return err
				}
				if len(attachments) > 0 {
					attachDir := filepath.Join(outputDir, "attachments")
					if err := os.MkdirAll(attachDir, 0o755); err != nil {
						return fmt.Errorf("ensure attachment dir: %w", err)
					}
					for _, att := range attachments {
						name := att.Name
						if strings.TrimSpace(name) == "" {
							name = att.MediaType
						}
						path := filepath.Join(attachDir, sanitizeName(name))
						if err := os.WriteFile(path, att.Data, 0o644); err != nil {
							return fmt.Errorf("write attachment %s: %w", name, err)
						}
					}
				}
			}

			abs, _ := filepath.Abs(outputDir)
			cmd.Printf("Unpacked %d manifests from snapshot %s to %s\n", len(manifests), snapshot.Name, abs)
			return nil
		},
	}

	cmd.Flags().StringVar(&archivePath, "archive-file", "app.k8s", "Path to the .k8s archive to unpack")
	cmd.Flags().StringVar(&archivePath, "input", "app.k8s", "(deprecated) use --archive-file")
	if f := cmd.Flags().Lookup("input"); f != nil {
		f.Deprecated = "use --archive-file"
		f.Hidden = true
	}
	cmd.Flags().StringVar(&snapshotName, "snapshot", "", "Snapshot name to unpack (defaults to latest)")
	cmd.Flags().StringVar(&outputDir, "output-dir", outputDir, "Directory where manifests/attachments are written")
	cmd.Flags().BoolVar(&includeAttach, "include-attachments", false, "Write attachment blobs alongside manifests")

	_ = cmd.MarkFlagFilename("archive-file")
	_ = cmd.MarkFlagDirname("output-dir")

	return cmd
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("/", "-", " ", "-", "_", "-", ":", "-", "[", "", "]", "")
	value = replacer.Replace(value)
	if value == "" {
		return "item"
	}
	return value
}
