// app_package.go implements 'ktl app package', capturing namespaces plus metadata into reproducible .k8s application archives.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	apparchive "github.com/example/ktl/internal/apparchive"
	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/gitinfo"
	"github.com/example/ktl/internal/images"
	"github.com/example/ktl/internal/signing"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

const (
	attachmentSBOM           = "sbom.spdx.json"
	attachmentProvenance     = "provenance.slsa.json"
	attachmentLicenseSummary = "license-summary.json"
	signingKeyEnv            = "KTL_SIGNING_KEY"
)

func newAppPackageCommand(namespace *string, kubeconfig *string, kubeContext *string) *cobra.Command {
	var (
		chart          string
		release        string
		version        string
		values         []string
		set            []string
		setString      []string
		setFile        []string
		archivePath    string
		kubeVersion    string
		notesPath      string
		snapshotName   string
		parentSnapshot string
		signingKeyPath string
		signaturePath  string
	)
	archivePath = "app.k8s"

	cmd := &cobra.Command{
		Use:     "package",
		Aliases: []string{"app-archive"},
		Short:   "Bundle manifests, images, and metadata into a portable .k8s archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			settings := cli.New()
			if kubeconfig != nil && *kubeconfig != "" {
				settings.KubeConfig = *kubeconfig
			}
			if kubeContext != nil && *kubeContext != "" {
				settings.KubeContext = *kubeContext
			}
			if namespace != nil && *namespace != "" {
				settings.SetNamespace(*namespace)
			}

			actionCfg := new(action.Configuration)
			logFunc := func(format string, v ...interface{}) {
				fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", v...)
			}
			if err := actionCfg.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), logFunc); err != nil {
				return fmt.Errorf("init helm config: %w", err)
			}

			templateResult, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
				Chart:           chart,
				Version:         version,
				ReleaseName:     release,
				Namespace:       settings.Namespace(),
				ValuesFiles:     values,
				SetValues:       set,
				SetStringValues: setString,
				SetFileValues:   setFile,
				IncludeCRDs:     true,
			})
			if err != nil {
				return err
			}

			layerName := snapshotName
			if strings.TrimSpace(layerName) == "" {
				layerName = fmt.Sprintf("%s-%s", release, time.Now().UTC().Format("20060102-150405"))
			}
			gitCommit, gitDirty := "", false
			if commit, dirty, err := gitinfo.Head(cmd.Context()); err == nil {
				gitCommit = commit
				gitDirty = dirty
			}
			builder, err := apparchive.NewBuilder(archivePath, apparchive.SnapshotMetadata{
				Name:         layerName,
				Parent:       parentSnapshot,
				Release:      release,
				Namespace:    settings.Namespace(),
				Chart:        chart,
				ChartVersion: templateResult.ChartVersion,
				KubeVersion:  kubeVersion,
				GitCommit:    gitCommit,
				GitDirty:     gitDirty,
			})
			if err != nil {
				return err
			}
			defer func() {
				if builder != nil {
					builder.Close()
				}
			}()

			if err := builder.SetMetadata(map[string]string{
				"release":      release,
				"namespace":    settings.Namespace(),
				"chart":        chart,
				"chartVersion": templateResult.ChartVersion,
				"renderedAt":   time.Now().UTC().Format(time.RFC3339),
				"kubeVersion":  kubeVersion,
				"snapshot":     layerName,
			}); err != nil {
				return err
			}

			docs := splitManifests(templateResult.Manifest)
			manifestCount := 0
			for key, doc := range docs {
				meta := manifestMeta(doc)
				checksum := sha256.Sum256([]byte(doc))
				rec := apparchive.ManifestRecord{
					ID:         key,
					APIVersion: meta.APIVersion,
					Kind:       meta.Kind,
					Namespace:  meta.Namespace,
					Name:       meta.Name,
					Body:       doc,
					Checksum:   hex.EncodeToString(checksum[:]),
				}
				if err := builder.AddManifest(rec); err != nil {
					return err
				}
				manifestCount++
			}

			refs, err := images.Extract(templateResult.Manifest)
			if err != nil {
				return err
			}
			imageStats, err := archiveImages(ctx, builder, refs)
			if err != nil {
				return err
			}

			if strings.TrimSpace(notesPath) != "" {
				data, err := os.ReadFile(notesPath)
				if err != nil {
					return fmt.Errorf("read notes: %w", err)
				}
				mediaType := "text/plain"
				if filepath.Ext(notesPath) == ".md" {
					mediaType = "text/markdown"
				}
				if err := builder.AddAttachment(apparchive.Attachment{
					Name:      filepath.Base(notesPath),
					MediaType: mediaType,
					Data:      data,
				}); err != nil {
					return err
				}
			}

			if err := writeLicenseSummaryAttachment(builder, imageStats); err != nil {
				return err
			}
			if err := writeSBOMAttachment(builder, release, layerName, imageStats); err != nil {
				return err
			}
			if err := writeProvenanceAttachment(builder, provenanceConfig{
				Release:         release,
				Snapshot:        layerName,
				Namespace:       settings.Namespace(),
				Chart:           chart,
				ChartVersion:    templateResult.ChartVersion,
				ValuesFiles:     values,
				SetValues:       set,
				SetStringValues: setString,
				SetFileValues:   setFile,
				ManifestCount:   manifestCount,
				ImageCount:      imageStats.Count,
				GitCommit:       gitCommit,
				GitDirty:        gitDirty,
				KubeVersion:     kubeVersion,
				ParentSnapshot:  parentSnapshot,
				Images:          imageStats.Entries,
			}); err != nil {
				return err
			}

			if builder != nil {
				if err := builder.Close(); err != nil {
					return err
				}
				builder = nil
			}

			privKey, pubKey, keyPath, pubPath, generatedKey, err := resolveSigningKey(signingKeyPath)
			if err != nil {
				return err
			}
			if generatedKey {
				fmt.Fprintf(cmd.ErrOrStderr(), "Generated new signing key at %s (public %s)\n", absOrSelf(keyPath), absOrSelf(pubPath))
			}
			sigPath := signaturePath
			if strings.TrimSpace(sigPath) == "" {
				sigPath = archivePath + ".sig"
			}
			env, err := signing.SignFile(archivePath, privKey, pubKey)
			if err != nil {
				return fmt.Errorf("sign archive: %w", err)
			}
			if err := signing.SaveEnvelope(sigPath, env); err != nil {
				return fmt.Errorf("write signature: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Snapshot %s wrote %d manifests and %d images to %s\n", layerName, manifestCount, imageStats.Count, absOrSelf(archivePath))
			fmt.Fprintf(cmd.OutOrStdout(), "Signature written to %s (key %s)\n", absOrSelf(sigPath), env.KeyID)
			if strings.TrimSpace(pubPath) != "" {
				if _, err := os.Stat(pubPath); err == nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Public key available at %s (derived from %s)\n", absOrSelf(pubPath), keyPath)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&chart, "chart", "", "Chart reference (path, repo/name, or OCI ref)")
	cmd.Flags().StringVar(&release, "release", "", "Release name used for templating")
	cmd.Flags().StringVar(&version, "version", "", "Chart version")
	cmd.Flags().StringSliceVarP(&values, "values", "f", nil, "Values files to include")
	cmd.Flags().StringArrayVar(&set, "set", nil, "Set values on the command line (key=val)")
	cmd.Flags().StringArrayVar(&setString, "set-string", nil, "Set STRING values on the command line")
	cmd.Flags().StringArrayVar(&setFile, "set-file", nil, "Set values from files (key=path)")
	cmd.Flags().StringVar(&archivePath, "archive-file", archivePath, "Path to the .k8s SQLite archive")
	cmd.Flags().StringVar(&archivePath, "output", archivePath, "(deprecated) use --archive-file")
	if f := cmd.Flags().Lookup("output"); f != nil {
		f.Deprecated = "use --archive-file"
		f.Hidden = true
	}
	cmd.Flags().StringVar(&kubeVersion, "kube-version", "", "Kubernetes version this archive targets (optional)")
	cmd.Flags().StringVar(&notesPath, "notes", "", "Path to a README/notes file to embed")
	cmd.Flags().StringVar(&snapshotName, "snapshot", "", "Name of the snapshot/layer stored in the archive (defaults to <release>-<timestamp>)")
	cmd.Flags().StringVar(&parentSnapshot, "parent-snapshot", "", "Optional parent snapshot name to build upon")
	cmd.Flags().StringVar(&signingKeyPath, "signing-key", "", "Path to the Ed25519 private key PEM used for signing (defaults to $KTL_SIGNING_KEY or ~/.config/ktl/signing_ed25519)")
	cmd.Flags().StringVar(&signaturePath, "signature-file", "", "Path for the detached archive signature (defaults to <archive>.sig)")

	_ = cmd.MarkFlagRequired("chart")
	_ = cmd.MarkFlagRequired("release")

	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "app package Flags")
	cmd.AddCommand(newAppPackageVerifyCommand())
	return cmd
}

func absOrSelf(path string) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

type licenseSummaryDocument struct {
	GeneratedAt   string              `json:"generatedAt"`
	Images        []imageLicenseEntry `json:"images"`
	LicenseTotals map[string]int      `json:"licenseTotals"`
}

type spdxDocument struct {
	SPDXVersion       string           `json:"spdxVersion"`
	DataLicense       string           `json:"dataLicense"`
	SPDXID            string           `json:"SPDXID"`
	Name              string           `json:"name"`
	DocumentNamespace string           `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo `json:"creationInfo"`
	Packages          []spdxPackage    `json:"packages"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name             string            `json:"name"`
	SPDXID           string            `json:"SPDXID"`
	VersionInfo      string            `json:"versionInfo,omitempty"`
	Supplier         string            `json:"supplier"`
	DownloadLocation string            `json:"downloadLocation"`
	FilesAnalyzed    bool              `json:"filesAnalyzed"`
	LicenseDeclared  string            `json:"licenseDeclared"`
	LicenseConcluded string            `json:"licenseConcluded"`
	ExternalRefs     []spdxExternalRef `json:"externalRefs,omitempty"`
}

type spdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type provenanceConfig struct {
	Release         string
	Snapshot        string
	Namespace       string
	Chart           string
	ChartVersion    string
	ValuesFiles     []string
	SetValues       []string
	SetStringValues []string
	SetFileValues   []string
	ManifestCount   int
	ImageCount      int
	GitCommit       string
	GitDirty        bool
	KubeVersion     string
	ParentSnapshot  string
	Images          []imageLicenseEntry
}

type provenanceStatement struct {
	Type        string               `json:"type"`
	SLSAVersion string               `json:"slsaVersion"`
	Subject     []provenanceSubject  `json:"subject"`
	Builder     provenanceBuilder    `json:"builder"`
	BuildType   string               `json:"buildType"`
	Invocation  provenanceInvocation `json:"invocation"`
	Metadata    provenanceMetadata   `json:"metadata"`
	Materials   []provenanceMaterial `json:"materials"`
}

type provenanceSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type provenanceBuilder struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type provenanceInvocation struct {
	Parameters map[string]interface{} `json:"parameters"`
}

type provenanceMetadata struct {
	BuildStartedOn    time.Time `json:"buildStartedOn"`
	BuildFinishedOn   time.Time `json:"buildFinishedOn"`
	ManifestCount     int       `json:"manifestCount"`
	ImageCount        int       `json:"imageCount"`
	ParentSnapshot    string    `json:"parentSnapshot,omitempty"`
	KubernetesVersion string    `json:"kubernetesVersion,omitempty"`
	GitDirty          bool      `json:"gitDirty"`
}

type provenanceMaterial struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest,omitempty"`
}

func writeLicenseSummaryAttachment(builder *apparchive.Builder, stats imageArchiveStats) error {
	if builder == nil || stats.Count == 0 {
		return nil
	}
	totals := make(map[string]int)
	for _, entry := range stats.Entries {
		for _, license := range entry.Licenses {
			totals[license]++
		}
	}
	doc := licenseSummaryDocument{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Images:        stats.Entries,
		LicenseTotals: totals,
	}
	if err := addJSONAttachment(builder, attachmentLicenseSummary, doc); err != nil {
		return err
	}
	return builder.SetMetadata(map[string]string{
		"licenseSummary": formatLicenseTotals(totals),
	})
}

func writeSBOMAttachment(builder *apparchive.Builder, release, snapshot string, stats imageArchiveStats) error {
	if builder == nil {
		return nil
	}
	now := time.Now().UTC()
	doc := spdxDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              fmt.Sprintf("%s:%s", release, snapshot),
		DocumentNamespace: fmt.Sprintf("https://ktl.sh/spdx/%s/%d", snapshot, now.Unix()),
		CreationInfo: spdxCreationInfo{
			Created: now.Format(time.RFC3339),
			Creators: []string{
				"Organization: ktl",
				fmt.Sprintf("Tool: ktl/%s", runtime.Version()),
			},
		},
		Packages: make([]spdxPackage, 0, len(stats.Entries)),
	}
	for idx, entry := range stats.Entries {
		pkg := spdxPackage{
			Name:             entry.Reference,
			SPDXID:           fmt.Sprintf("SPDXRef-Package-%d", idx+1),
			VersionInfo:      entry.Digest,
			Supplier:         "NOASSERTION",
			DownloadLocation: "NOASSERTION",
			FilesAnalyzed:    false,
			LicenseDeclared:  licenseStringOrNoAssertion(entry.Licenses),
			LicenseConcluded: licenseStringOrNoAssertion(entry.Licenses),
		}
		if locator := imagePURL(entry); locator != "" {
			pkg.ExternalRefs = []spdxExternalRef{{
				ReferenceCategory: "PACKAGE-MANAGER",
				ReferenceType:     "purl",
				ReferenceLocator:  locator,
			}}
		}
		doc.Packages = append(doc.Packages, pkg)
	}
	return addJSONAttachment(builder, attachmentSBOM, doc)
}

func writeProvenanceAttachment(builder *apparchive.Builder, cfg provenanceConfig) error {
	if builder == nil {
		return nil
	}
	now := time.Now().UTC()
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%d-%d", cfg.Release, cfg.Snapshot, cfg.ManifestCount, cfg.ImageCount)))
	subject := provenanceSubject{
		Name: fmt.Sprintf("%s:%s", cfg.Release, cfg.Snapshot),
		Digest: map[string]string{
			"sha256": hex.EncodeToString(sum[:]),
		},
	}
	params := map[string]interface{}{
		"chart":             cfg.Chart,
		"chartVersion":      cfg.ChartVersion,
		"release":           cfg.Release,
		"namespace":         cfg.Namespace,
		"valuesFiles":       cfg.ValuesFiles,
		"set":               cfg.SetValues,
		"setString":         cfg.SetStringValues,
		"setFile":           cfg.SetFileValues,
		"gitCommit":         cfg.GitCommit,
		"kubernetesVersion": cfg.KubeVersion,
	}
	statement := provenanceStatement{
		Type:        "https://slsa.dev/provenance/v1",
		SLSAVersion: "1.0",
		Subject:     []provenanceSubject{subject},
		Builder: provenanceBuilder{
			ID:      "ktl/app-package",
			Version: runtime.Version(),
		},
		BuildType:  "ktl.app.package/v1",
		Invocation: provenanceInvocation{Parameters: params},
		Metadata: provenanceMetadata{
			BuildStartedOn:    now,
			BuildFinishedOn:   now,
			ManifestCount:     cfg.ManifestCount,
			ImageCount:        cfg.ImageCount,
			ParentSnapshot:    cfg.ParentSnapshot,
			KubernetesVersion: cfg.KubeVersion,
			GitDirty:          cfg.GitDirty,
		},
	}
	materials := []provenanceMaterial{
		{URI: fmt.Sprintf("chart:%s@%s", cfg.Chart, cfg.ChartVersion)},
	}
	if strings.TrimSpace(cfg.GitCommit) != "" {
		digest := strings.TrimSpace(cfg.GitCommit)
		materials = append(materials, provenanceMaterial{
			URI: fmt.Sprintf("git:%s", digest),
			Digest: map[string]string{
				"sha1": digest,
			},
		})
	}
	for _, entry := range cfg.Images {
		materials = append(materials, provenanceMaterial{
			URI: fmt.Sprintf("container:%s", entry.Reference),
			Digest: map[string]string{
				"sha256": strings.TrimPrefix(entry.Digest, "sha256:"),
			},
		})
	}
	statement.Materials = materials
	return addJSONAttachment(builder, attachmentProvenance, statement)
}

func addJSONAttachment(builder *apparchive.Builder, name string, payload interface{}) error {
	if builder == nil || payload == nil {
		return nil
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return builder.AddAttachment(apparchive.Attachment{
		Name:      name,
		MediaType: "application/json",
		Data:      data,
	})
}

func licenseStringOrNoAssertion(values []string) string {
	if len(values) == 0 {
		return "NOASSERTION"
	}
	return strings.Join(values, " AND ")
}

func imagePURL(entry imageLicenseEntry) string {
	ref, err := name.ParseReference(entry.Reference)
	if err != nil {
		return ""
	}
	purl := fmt.Sprintf("pkg:docker/%s", ref.Context().RepositoryStr())
	if tagged, ok := ref.(name.Tag); ok {
		purl += ":" + tagged.TagStr()
	} else if digested, ok := ref.(name.Digest); ok {
		purl += "@" + digested.DigestStr()
	}
	digest := strings.TrimPrefix(entry.Digest, "sha256:")
	if digest != "" {
		purl += "?digest=" + digest
	}
	return purl
}

func formatLicenseTotals(totals map[string]int) string {
	if len(totals) == 0 {
		return ""
	}
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, totals[key]))
	}
	return strings.Join(parts, ",")
}

type imageArchiveStats struct {
	Count   int
	Entries []imageLicenseEntry
}

type imageLicenseEntry struct {
	Reference string   `json:"reference"`
	Digest    string   `json:"digest"`
	Licenses  []string `json:"licenses"`
	Source    string   `json:"source"`
}

func archiveImages(ctx context.Context, builder *apparchive.Builder, refs []string) (imageArchiveStats, error) {
	stats := imageArchiveStats{}
	if len(refs) == 0 {
		return stats, nil
	}
	seen := make(map[string]struct{})
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		payload, err := buildImageRecord(ctx, ref)
		if err != nil {
			return stats, err
		}
		if err := builder.AddImage(payload.Record); err != nil {
			return stats, err
		}
		stats.Count++
		stats.Entries = append(stats.Entries, payload.License)
	}
	return stats, nil
}

type imageRecordPayload struct {
	Record  apparchive.ImageRecord
	License imageLicenseEntry
}

func buildImageRecord(ctx context.Context, ref string) (imageRecordPayload, error) {
	img, err := crane.Pull(ref, crane.WithContext(ctx))
	if err != nil {
		return imageRecordPayload{}, fmt.Errorf("pull %s: %w", ref, err)
	}
	manifestBytes, err := img.RawManifest()
	if err != nil {
		return imageRecordPayload{}, fmt.Errorf("manifest %s: %w", ref, err)
	}
	manifest, err := img.Manifest()
	if err != nil {
		return imageRecordPayload{}, err
	}
	imgDigest, err := img.Digest()
	if err != nil {
		return imageRecordPayload{}, err
	}
	configBytes, err := img.RawConfigFile()
	if err != nil {
		return imageRecordPayload{}, err
	}
	configMediaType := string(manifest.Config.MediaType)
	if configMediaType == "" {
		configMediaType = "application/vnd.oci.image.config.v1+json"
	}
	layers := make([]apparchive.ImageLayerRecord, 0, len(manifest.Layers))
	for idx, desc := range manifest.Layers {
		layer, err := img.LayerByDigest(desc.Digest)
		if err != nil {
			return imageRecordPayload{}, err
		}
		rc, err := layer.Compressed()
		if err != nil {
			return imageRecordPayload{}, err
		}
		data, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr != nil {
			return imageRecordPayload{}, readErr
		}
		mediaType := string(desc.MediaType)
		if mediaType == "" {
			mediaType = "application/vnd.oci.image.layer.v1.tar+gzip"
		}
		layers = append(layers, apparchive.ImageLayerRecord{
			Blob: apparchive.BlobRecord{
				Digest:    desc.Digest.String(),
				MediaType: mediaType,
				Data:      data,
			},
			Order: idx,
		})
	}
	manifestMedia := string(manifest.MediaType)
	if manifestMedia == "" {
		manifestMedia = "application/vnd.oci.image.manifest.v1+json"
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return imageRecordPayload{}, err
	}
	labels := map[string]string{}
	if cfg != nil && cfg.Config.Labels != nil {
		labels = cfg.Config.Labels
	}
	licenseEntry := imageLicenseEntry{
		Reference: ref,
		Digest:    imgDigest.String(),
		Licenses:  extractImageLicenses(labels),
		Source:    detectLicenseSource(labels),
	}
	return imageRecordPayload{
		Record: apparchive.ImageRecord{
			Reference: ref,
			Manifest: apparchive.BlobRecord{
				Digest:    imgDigest.String(),
				MediaType: manifestMedia,
				Data:      manifestBytes,
			},
			Config: apparchive.BlobRecord{
				Digest:    manifest.Config.Digest.String(),
				MediaType: configMediaType,
				Data:      configBytes,
			},
			Layers: layers,
		},
		License: licenseEntry,
	}, nil
}

func resolveSigningKey(flagPath string) (ed25519.PrivateKey, ed25519.PublicKey, string, string, bool, error) {
	flagPath = strings.TrimSpace(flagPath)
	if flagPath != "" {
		priv, pub, err := loadPrivateKeyStrict(flagPath)
		return priv, pub, flagPath, "", false, err
	}
	if envPath := strings.TrimSpace(os.Getenv(signingKeyEnv)); envPath != "" {
		priv, pub, err := loadPrivateKeyStrict(envPath)
		return priv, pub, envPath, "", false, err
	}
	privDefault, pubDefault := defaultSigningKeyPaths()
	if priv, pub, err := loadPrivateKeyOptional(privDefault); err != nil {
		return nil, nil, "", "", false, err
	} else if priv != nil {
		return priv, pub, privDefault, pubDefault, false, nil
	}
	priv, pub, err := signing.GenerateKeyPair()
	if err != nil {
		return nil, nil, "", "", false, err
	}
	if err := signing.SavePrivateKey(privDefault, priv); err != nil {
		return nil, nil, "", "", false, err
	}
	if err := signing.SavePublicKey(pubDefault, pub); err != nil {
		return nil, nil, "", "", false, err
	}
	return priv, pub, privDefault, pubDefault, true, nil
}

func loadPrivateKeyStrict(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil, fmt.Errorf("signing key %s: %w", path, err)
	}
	priv, pub, err := signing.LoadPrivateKey(path)
	return priv, pub, err
}

func loadPrivateKeyOptional(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if path == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return signing.LoadPrivateKey(path)
}

func defaultSigningKeyPaths() (string, string) {
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	targetDir := filepath.Join(configDir, "ktl")
	return filepath.Join(targetDir, "signing_ed25519"), filepath.Join(targetDir, "signing_ed25519.pub")
}

func extractImageLicenses(labels map[string]string) []string {
	if len(labels) == 0 {
		return []string{"UNKNOWN"}
	}
	candidates := []string{"org.opencontainers.image.licenses", "licenses", "license"}
	for _, key := range candidates {
		if raw, ok := labels[key]; ok && strings.TrimSpace(raw) != "" {
			return parseLicenseList(raw)
		}
	}
	return []string{"UNKNOWN"}
}

func detectLicenseSource(labels map[string]string) string {
	if len(labels) == 0 {
		return "unknown"
	}
	if _, ok := labels["org.opencontainers.image.licenses"]; ok {
		return "org.opencontainers.image.licenses"
	}
	if _, ok := labels["licenses"]; ok {
		return "licenses"
	}
	if _, ok := labels["license"]; ok {
		return "license"
	}
	return "unknown"
}

func parseLicenseList(raw string) []string {
	split := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';'
	})
	var licenses []string
	for _, item := range split {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		licenses = append(licenses, item)
	}
	if len(licenses) == 0 {
		return []string{"UNKNOWN"}
	}
	return licenses
}
