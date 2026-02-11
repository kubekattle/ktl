package verify

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Minimal SARIF 2.1.0 emitter for GitHub code scanning / CI consumers.
// We keep this intentionally small and stable: tool metadata + rule index + results.

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool      sarifTool       `json:"tool"`
	Invoc     []sarifInvoc    `json:"invocations,omitempty"`
	Results   []sarifResult   `json:"results,omitempty"`
	Artifacts []sarifArtifact `json:"artifacts,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID               string         `json:"id"`
	Name             string         `json:"name,omitempty"`
	HelpURI          string         `json:"helpUri,omitempty"`
	ShortDescription sarifMessage   `json:"shortDescription,omitempty"`
	Properties       map[string]any `json:"properties,omitempty"`
}

type sarifInvoc struct {
	ExecutionSuccessful bool      `json:"executionSuccessful"`
	EndTimeUTC          time.Time `json:"endTimeUtc,omitempty"`
}

type sarifResult struct {
	RuleID    string         `json:"ruleId"`
	Level     string         `json:"level,omitempty"`
	Message   sarifMessage   `json:"message"`
	Locations []sarifLocWrap `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocWrap struct {
	PhysicalLocation sarifPhysLoc `json:"physicalLocation"`
}

type sarifPhysLoc struct {
	ArtifactLocation sarifArtifactLoc `json:"artifactLocation"`
	Region           sarifRegion      `json:"region,omitempty"`
}

type sarifArtifactLoc struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine,omitempty"`
}

type sarifArtifact struct {
	Location sarifArtifactLoc `json:"location"`
}

func ToSARIF(rep *Report) ([]byte, error) {
	if rep == nil {
		return nil, nil
	}
	ruleIndex := map[string]sarifRule{}
	for _, f := range rep.Findings {
		if _, ok := ruleIndex[f.RuleID]; ok {
			continue
		}
		r := sarifRule{
			ID:   f.RuleID,
			Name: f.RuleID,
			ShortDescription: sarifMessage{
				Text: firstNonEmpty(f.Category, f.RuleID),
			},
		}
		if f.HelpURL != "" {
			r.HelpURI = f.HelpURL
		}
		r.Properties = map[string]any{
			"category": f.Category,
		}
		ruleIndex[f.RuleID] = r
	}
	rules := make([]sarifRule, 0, len(ruleIndex))
	for _, r := range ruleIndex {
		rules = append(rules, r)
	}
	// stable-ish ordering
	sortSARIFRules(rules)

	artifacts := map[string]bool{}
	var results []sarifResult
	for _, f := range rep.Findings {
		res := sarifResult{
			RuleID:  f.RuleID,
			Level:   sarifLevel(f.Severity),
			Message: sarifMessage{Text: firstNonEmpty(f.Message, f.RuleID)},
		}
		path := strings.TrimSpace(f.Path)
		if path != "" {
			artifacts[path] = true
			loc := sarifLocWrap{
				PhysicalLocation: sarifPhysLoc{
					ArtifactLocation: sarifArtifactLoc{URI: path},
				},
			}
			if f.Line > 0 {
				loc.PhysicalLocation.Region = sarifRegion{StartLine: f.Line}
			}
			res.Locations = []sarifLocWrap{loc}
		}
		results = append(results, res)
	}

	var sarifArtifacts []sarifArtifact
	for uri := range artifacts {
		sarifArtifacts = append(sarifArtifacts, sarifArtifact{Location: sarifArtifactLoc{URI: uri}})
	}
	sortSARIFArtifacts(sarifArtifacts)

	log := sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "verify",
						Version:        rep.Engine.Version,
						InformationURI: "https://github.com/avkcode/ktl",
						Rules:          rules,
					},
				},
				Invoc:     []sarifInvoc{{ExecutionSuccessful: true, EndTimeUTC: rep.EvaluatedAt}},
				Results:   results,
				Artifacts: sarifArtifacts,
			},
		},
	}
	return json.MarshalIndent(log, "", "  ")
}

func sarifLevel(sev Severity) string {
	switch sev {
	case SeverityCritical, SeverityHigh:
		return "error"
	case SeverityMedium:
		return "warning"
	default:
		return "note"
	}
}

func sortSARIFRules(rules []sarifRule) {
	for i := 0; i < len(rules); i++ {
		for j := i + 1; j < len(rules); j++ {
			if rules[j].ID < rules[i].ID {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}
}

func sortSARIFArtifacts(arts []sarifArtifact) {
	for i := 0; i < len(arts); i++ {
		for j := i + 1; j < len(arts); j++ {
			if arts[j].Location.URI < arts[i].Location.URI {
				arts[i], arts[j] = arts[j], arts[i]
			}
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func validateSARIF(rep *Report) error {
	if rep == nil {
		return nil
	}
	for _, f := range rep.Findings {
		if strings.TrimSpace(f.RuleID) == "" {
			return fmt.Errorf("sarif: finding missing ruleId")
		}
	}
	return nil
}
