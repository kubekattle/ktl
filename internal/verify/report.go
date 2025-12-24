package verify

import "time"

type Mode string

const (
	ModeWarn  Mode = "warn"
	ModeBlock Mode = "block"
	ModeOff   Mode = "off"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

type Subject struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

type Finding struct {
	RuleID      string   `json:"ruleId"`
	Severity    Severity `json:"severity"`
	Category    string   `json:"category,omitempty"`
	Message     string   `json:"message"`
	Path        string   `json:"path,omitempty"`
	Line        int      `json:"line,omitempty"`
	Location    string   `json:"location,omitempty"`
	Subject     Subject  `json:"subject,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	HelpURL     string   `json:"helpUrl,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type Summary struct {
	Total   int              `json:"total"`
	BySev   map[Severity]int `json:"bySeverity,omitempty"`
	Passed  bool             `json:"passed"`
	Blocked bool             `json:"blocked"`
}

type EngineMeta struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Ruleset string `json:"ruleset,omitempty"`
}

type Input struct {
	Kind            string `json:"kind,omitempty"` // chart|namespace
	Chart           string `json:"chart,omitempty"`
	Release         string `json:"release,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	RenderedSHA256  string `json:"renderedSha256,omitempty"`
	CollectedAtHint string `json:"collectedAtHint,omitempty"`
}

type Report struct {
	Tool        string     `json:"tool"`
	Engine      EngineMeta `json:"engine"`
	Mode        Mode       `json:"mode"`
	Passed      bool       `json:"passed"`
	Blocked     bool       `json:"blocked"`
	EvaluatedAt time.Time  `json:"evaluatedAt"`
	Inputs      []Input    `json:"inputs,omitempty"`
	Summary     Summary    `json:"summary"`
	Findings    []Finding  `json:"findings,omitempty"`
}
