package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	buildModeFull       = "full"
	buildModeLogsOnly   = "logs-only"
	buildModeHelmerOnly = "helmer-only"
)

type cliProfile struct {
	Name                 string
	PlanCommandLabel     string
	PlanHTMLPrefix       string
	PlanVisualizePrefix  string
	PlanInstallCmdWords  []string
	PlanHTMLDefaultLabel string
}

func normalizedBuildMode() string {
	mode := strings.ToLower(strings.TrimSpace(buildMode))
	if mode == "" {
		return buildModeFull
	}
	return mode
}

func isLogsOnlyBuild() bool {
	return normalizedBuildMode() == buildModeLogsOnly
}

func isHelmerOnlyBuild() bool {
	return normalizedBuildMode() == buildModeHelmerOnly
}

func currentCLIProfile() cliProfile {
	if isHelmerOnlyBuild() {
		return cliProfile{
			Name:                 "helmer",
			PlanCommandLabel:     "helmer plan",
			PlanHTMLPrefix:       "helmer-plan",
			PlanVisualizePrefix:  "helmer-plan",
			PlanInstallCmdWords:  []string{"helmer", "plan"},
			PlanHTMLDefaultLabel: "./helmer-plan-<release>-<timestamp>.html",
		}
	}
	return cliProfile{
		Name:                 "torque",
		PlanCommandLabel:     "torque apply plan",
		PlanHTMLPrefix:       "torque-deploy-plan",
		PlanVisualizePrefix:  "torque-deploy-visualize",
		PlanInstallCmdWords:  []string{"torque", "apply", "plan"},
		PlanHTMLDefaultLabel: "./torque-deploy-plan-<release>-<timestamp>.html",
	}
}

func currentCLIName() string {
	return currentCLIProfile().Name
}

func defaultPlanHTMLOutputPath(release string, generatedAt time.Time) string {
	profile := currentCLIProfile()
	slug := sanitizeFilename(release)
	if slug == "" {
		slug = "release"
	}
	stamp := generatedAt
	if stamp.IsZero() {
		stamp = time.Now().UTC()
	}
	return fmt.Sprintf("%s-%s-%s.html", profile.PlanHTMLPrefix, slug, stamp.Format("20060102-150405"))
}

func defaultPlanVisualizeDataOutputPath(release string, generatedAt time.Time, ext string) string {
	profile := currentCLIProfile()
	slug := sanitizeFilename(release)
	if slug == "" {
		slug = "release"
	}
	stamp := generatedAt
	if stamp.IsZero() {
		stamp = time.Now().UTC()
	}
	if strings.TrimSpace(ext) == "" {
		ext = "json"
	}
	return fmt.Sprintf("%s-%s-%s.%s", profile.PlanVisualizePrefix, slug, stamp.Format("20060102-150405"), ext)
}

func defaultPlanVisualizeHTMLOutputPath(release string, generatedAt time.Time) string {
	return defaultPlanVisualizeDataOutputPath(release, generatedAt, "html")
}

func rewritePlanSurfaceBranding(text string) string {
	if !isHelmerOnlyBuild() || text == "" {
		return text
	}
	profile := currentCLIProfile()
	replacer := strings.NewReplacer(
		"torque apply plan", profile.PlanCommandLabel,
		"torque-deploy-visualize-", profile.PlanVisualizePrefix+"-",
		"torque-deploy-plan-", profile.PlanHTMLPrefix+"-",
	)
	return replacer.Replace(text)
}
