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
	RuleID      string         `json:"ruleId"`
	Severity    Severity       `json:"severity"`
	Category    string         `json:"category,omitempty"`
	Message     string         `json:"message"`
	FieldPath   string         `json:"fieldPath,omitempty"`
	Path        string         `json:"path,omitempty"`
	Line        int            `json:"line,omitempty"`
	Location    string         `json:"location,omitempty"`
	ResourceKey string         `json:"resourceKey,omitempty"`
	Expected    string         `json:"expected,omitempty"`
	Observed    string         `json:"observed,omitempty"`
	Subject     Subject        `json:"subject,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	HelpURL     string         `json:"helpUrl,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Evidence    map[string]any `json:"evidence,omitempty"`
}

type Summary struct {
	Total          int                         `json:"total"`
	BySev          map[Severity]int            `json:"bySeverity,omitempty"`
	ByRule         map[string]int              `json:"byRule,omitempty"`
	ByRuleSeverity map[string]map[Severity]int `json:"byRuleSeverity,omitempty"`
	Passed         bool                        `json:"passed"`
	Blocked        bool                        `json:"blocked"`
}

type EngineMeta struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Ruleset string `json:"ruleset,omitempty"`
}

type Input struct {
	Kind            string `json:"kind,omitempty"` // chart|namespace|manifest
	Source          string `json:"source,omitempty"`
	Chart           string `json:"chart,omitempty"`
	Release         string `json:"release,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	RenderedSHA256  string `json:"renderedSha256,omitempty"`
	CollectedAtHint string `json:"collectedAtHint,omitempty"`
}

type DeltaReport struct {
	BaselineTotal int `json:"baselineTotal,omitempty"`
	Unchanged     int `json:"unchanged,omitempty"`

	// NewOrChanged and Fixed are kept for backwards compatibility with older
	// reports/consumers.
	NewOrChanged []Finding `json:"newOrChanged,omitempty"`
	Fixed        []Finding `json:"fixed,omitempty"`

	// NewOrChangedDetails and FixedDetails provide a change narrative for compare-to:
	// what changed (message/observed/expected/severity/etc) and a snapshot of the
	// baseline finding.
	NewOrChangedDetails []DeltaDetail `json:"newOrChangedDetails,omitempty"`
	FixedDetails        []DeltaDetail `json:"fixedDetails,omitempty"`
}

type Report struct {
	Tool        string          `json:"tool"`
	Engine      EngineMeta      `json:"engine"`
	Mode        Mode            `json:"mode"`
	FailOn      Severity        `json:"failOn,omitempty"`
	Passed      bool            `json:"passed"`
	Blocked     bool            `json:"blocked"`
	EvaluatedAt time.Time       `json:"evaluatedAt"`
	Inputs      []Input         `json:"inputs,omitempty"`
	Summary     Summary         `json:"summary"`
	Findings    []Finding       `json:"findings,omitempty"`
	Delta       *DeltaReport    `json:"delta,omitempty"`
	Exposure    *ExposureReport `json:"exposure,omitempty"`
}
