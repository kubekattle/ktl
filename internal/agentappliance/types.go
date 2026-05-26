package agentappliance

import "time"

const (
	// Version is the evidence schema version emitted by the agent appliance runner.
	Version = "v1"
	// ToolName is the stable tool identifier written into manifests.
	ToolName = "torque-agent-appliance"
)

// Options controls one agent appliance evidence run.
type Options struct {
	RepoDir        string
	OutDir         string
	Task           string
	Actor          string
	BrowserMode    string
	BrowserURLs    []string
	APIURLs        []string
	Checks         []string
	Timeout        time.Duration
	MaxOutputBytes int
	Now            func() time.Time
}

// Report is the top-level manifest written to manifest.json and returned to callers.
type Report struct {
	Version          string         `json:"version"`
	GeneratedAt      string         `json:"generatedAt"`
	Tool             string         `json:"tool"`
	Actor            string         `json:"actor"`
	Task             string         `json:"task,omitempty"`
	Passed           bool           `json:"passed"`
	OutDir           string         `json:"outDir"`
	RedactionEnabled bool           `json:"redactionEnabled"`
	RawSecretStored  bool           `json:"rawSecretStored"`
	Summary          Summary        `json:"summary"`
	Repo             RepoReport     `json:"repo"`
	API              APIReport      `json:"api"`
	Browser          BrowserReport  `json:"browser"`
	Checks           ChecksReport   `json:"checks"`
	Evidence         []EvidenceFile `json:"evidence"`
	Warnings         []string       `json:"warnings,omitempty"`
	ManifestPath     string         `json:"manifestPath"`
	ManifestSHA256   string         `json:"manifestSha256,omitempty"`
	SummaryPath      string         `json:"summaryPath"`
}

// Summary keeps the manifest scannable for CI, PR comments, and agent planning.
type Summary struct {
	RepoFiles             int `json:"repoFiles"`
	ChangedFiles          int `json:"changedFiles"`
	DependencyManifests   int `json:"dependencyManifests"`
	TestFiles             int `json:"testFiles"`
	APIProbes             int `json:"apiProbes"`
	APIPassed             int `json:"apiPassed"`
	APIFailed             int `json:"apiFailed"`
	BrowserProbes         int `json:"browserProbes"`
	BrowserCaptured       int `json:"browserCaptured"`
	BrowserSkipped        int `json:"browserSkipped"`
	BrowserFailed         int `json:"browserFailed"`
	CommandChecks         int `json:"commandChecks"`
	CommandChecksPassed   int `json:"commandChecksPassed"`
	CommandChecksFailed   int `json:"commandChecksFailed"`
	CommandChecksTimedOut int `json:"commandChecksTimedOut"`
}

type EvidenceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type RepoReport struct {
	Root                string         `json:"root"`
	VCS                 string         `json:"vcs,omitempty"`
	SearchTool          string         `json:"searchTool,omitempty"`
	Git                 GitReport      `json:"git"`
	FileCount           int            `json:"fileCount"`
	LanguageCounts      map[string]int `json:"languageCounts"`
	DependencyManifests []string       `json:"dependencyManifests"`
	TestFiles           []string       `json:"testFiles"`
	ChangedFiles        []string       `json:"changedFiles"`
	Impact              []ImpactEntry  `json:"impact"`
	Warnings            []string       `json:"warnings,omitempty"`
}

type GitReport struct {
	InRepo       bool     `json:"inRepo"`
	Root         string   `json:"root,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Dirty        bool     `json:"dirty"`
	Status       string   `json:"status,omitempty"`
	ChangedFiles []string `json:"changedFiles,omitempty"`
}

type ImpactEntry struct {
	ChangedFile string   `json:"changedFile"`
	TestFiles   []string `json:"testFiles,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

type APIReport struct {
	Total  int        `json:"total"`
	Passed int        `json:"passed"`
	Failed int        `json:"failed"`
	Probes []APIProbe `json:"probes,omitempty"`
}

type APIProbe struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	StatusCode     int               `json:"statusCode,omitempty"`
	DurationMillis int64             `json:"durationMillis"`
	ContentType    string            `json:"contentType,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	CapturedSHA256 string            `json:"capturedSha256,omitempty"`
	BodyPreview    string            `json:"bodyPreview,omitempty"`
	BodyTruncated  bool              `json:"bodyTruncated,omitempty"`
	Passed         bool              `json:"passed"`
	Error          string            `json:"error,omitempty"`
}

type BrowserReport struct {
	Mode                string           `json:"mode"`
	Total               int              `json:"total"`
	Captured            int              `json:"captured"`
	Skipped             int              `json:"skipped"`
	Failed              int              `json:"failed"`
	PlaywrightAvailable bool             `json:"playwrightAvailable"`
	Captures            []BrowserCapture `json:"captures,omitempty"`
}

type BrowserCapture struct {
	URL            string   `json:"url"`
	StatusCode     int      `json:"statusCode,omitempty"`
	DurationMillis int64    `json:"durationMillis,omitempty"`
	ScreenshotPath string   `json:"screenshotPath,omitempty"`
	DOMPath        string   `json:"domPath,omitempty"`
	HARPath        string   `json:"harPath,omitempty"`
	ConsolePath    string   `json:"consolePath,omitempty"`
	Passed         bool     `json:"passed"`
	Skipped        bool     `json:"skipped"`
	Reason         string   `json:"reason,omitempty"`
	Error          string   `json:"error,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

type ChecksReport struct {
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	TimedOut int           `json:"timedOut"`
	Results  []CheckResult `json:"results,omitempty"`
}

type CheckResult struct {
	Command         string `json:"command"`
	ExitCode        int    `json:"exitCode"`
	DurationMillis  int64  `json:"durationMillis"`
	TimedOut        bool   `json:"timedOut"`
	Passed          bool   `json:"passed"`
	StdoutPath      string `json:"stdoutPath,omitempty"`
	StderrPath      string `json:"stderrPath,omitempty"`
	StdoutSHA256    string `json:"stdoutSha256,omitempty"`
	StderrSHA256    string `json:"stderrSha256,omitempty"`
	StdoutPreview   string `json:"stdoutPreview,omitempty"`
	StderrPreview   string `json:"stderrPreview,omitempty"`
	StdoutTruncated bool   `json:"stdoutTruncated,omitempty"`
	StderrTruncated bool   `json:"stderrTruncated,omitempty"`
	Error           string `json:"error,omitempty"`
}
