// app_package_verify.go backs 'ktl app package verify', checking signed .k8s archives and their attestations against user-provided keys.
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	apparchive "github.com/example/ktl/internal/apparchive"
	"github.com/example/ktl/internal/signing"
	"github.com/spf13/cobra"
)

func newAppPackageVerifyCommand() *cobra.Command {
	var (
		archivePath   string
		signaturePath string
		publicKeyPath string
		snapshotName  string
	)
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a signed .k8s archive and its embedded attestations",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(signaturePath) == "" {
				signaturePath = archivePath + ".sig"
			}
			env, err := signing.LoadEnvelope(signaturePath)
			if err != nil {
				return fmt.Errorf("load signature: %w", err)
			}
			pubKey, pubPath, err := resolveVerificationPublicKey(publicKeyPath)
			if err != nil {
				return err
			}
			if err := signing.VerifyFile(archivePath, env, pubKey); err != nil {
				return fmt.Errorf("verify signature: %w", err)
			}

			reader, err := apparchive.NewReader(archivePath)
			if err != nil {
				return err
			}
			defer reader.Close()
			snapshot, err := reader.ResolveSnapshot(snapshotName)
			if err != nil {
				return err
			}
			attachments, err := reader.ReadAttachments(snapshot)
			if err != nil {
				return err
			}
			required := map[string]bool{
				attachmentProvenance:     false,
				attachmentSBOM:           false,
				attachmentLicenseSummary: false,
			}
			store := make(map[string][]byte, len(attachments))
			for _, att := range attachments {
				store[att.Name] = att.Data
				if _, ok := required[att.Name]; ok {
					required[att.Name] = true
				}
			}
			for name, ok := range required {
				if !ok {
					return fmt.Errorf("attachment %s missing from snapshot %s", name, snapshot.Name)
				}
			}
			if err := validateJSONAttachment(store[attachmentProvenance], &provenanceStatement{}); err != nil {
				return fmt.Errorf("provenance attachment invalid: %w", err)
			}
			if err := validateJSONAttachment(store[attachmentSBOM], &spdxDocument{}); err != nil {
				return fmt.Errorf("sbom attachment invalid: %w", err)
			}
			if err := validateJSONAttachment(store[attachmentLicenseSummary], &licenseSummaryDocument{}); err != nil {
				return fmt.Errorf("license summary attachment invalid: %w", err)
			}
			cmd.Printf("Archive %s verified (snapshot %s, signature %s, public key %s)\n",
				absOrSelf(archivePath), snapshot.Name, env.KeyID, absOrSelf(pubPath))
			return nil
		},
	}
	cmd.Flags().StringVar(&archivePath, "archive-file", "app.k8s", "Path to the archive to verify")
	cmd.Flags().StringVar(&signaturePath, "signature-file", "", "Detached signature file path (defaults to <archive>.sig)")
	cmd.Flags().StringVar(&publicKeyPath, "public-key", "", "Path to the Ed25519 public key PEM (defaults to ~/.config/ktl/signing_ed25519.pub)")
	cmd.Flags().StringVar(&snapshotName, "snapshot", "", "Snapshot name to inspect (defaults to latest)")
	_ = cmd.MarkFlagFilename("archive-file")
	_ = cmd.MarkFlagFilename("signature-file")
	_ = cmd.MarkFlagFilename("public-key")
	return cmd
}

func resolveVerificationPublicKey(path string) (ed25519.PublicKey, string, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		pub, err := loadPublicKeyStrict(path)
		return pub, path, err
	}
	_, defaultPub := defaultSigningKeyPaths()
	pub, err := loadPublicKeyStrict(defaultPub)
	return pub, defaultPub, err
}

func loadPublicKeyStrict(path string) (ed25519.PublicKey, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("public key %s: %w", path, err)
	}
	return signing.LoadPublicKey(path)
}

func validateJSONAttachment(data []byte, target interface{}) error {
	if len(data) == 0 {
		return fmt.Errorf("attachment empty")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return nil
}
