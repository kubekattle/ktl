package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Rule struct {
	ID          string
	Title       string
	Severity    Severity
	Category    string
	Description string
	HelpURL     string
	Dir         string
}

type Ruleset struct {
	Dir   string
	Rules []Rule
}

type kicsMetadata struct {
	ID              string `json:"id"`
	QueryName       string `json:"queryName"`
	Severity        string `json:"severity"`
	Category        string `json:"category"`
	DescriptionText string `json:"descriptionText"`
	DescriptionURL  string `json:"descriptionUrl"`
}

func LoadRuleset(dir string) (Ruleset, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return Ruleset{}, errors.New("rules dir is required")
	}
	base := filepath.Clean(dir)
	k8sDir := filepath.Join(base, "k8s")
	entries, err := os.ReadDir(k8sDir)
	if err != nil {
		return Ruleset{}, fmt.Errorf("read rules: %w", err)
	}
	var rules []Rule
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name := strings.TrimSpace(ent.Name())
		if name == "" {
			continue
		}
		ruleDir := filepath.Join(k8sDir, name)
		raw, err := os.ReadFile(filepath.Join(ruleDir, "metadata.json"))
		if err != nil {
			continue
		}
		var meta kicsMetadata
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		rules = append(rules, Rule{
			ID:          "k8s/" + name,
			Title:       strings.TrimSpace(meta.QueryName),
			Severity:    mapSeverity(meta.Severity),
			Category:    strings.TrimSpace(meta.Category),
			Description: strings.TrimSpace(meta.DescriptionText),
			HelpURL:     strings.TrimSpace(meta.DescriptionURL),
			Dir:         ruleDir,
		})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return Ruleset{Dir: base, Rules: rules}, nil
}

func mapSeverity(v string) Severity {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "CRITICAL":
		return SeverityCritical
	case "HIGH":
		return SeverityHigh
	case "MEDIUM":
		return SeverityMedium
	case "LOW":
		return SeverityLow
	default:
		return SeverityInfo
	}
}
