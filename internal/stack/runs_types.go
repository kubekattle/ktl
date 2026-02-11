package stack

// RunIndexEntry is a compact summary of a run, used by `ktl stack runs`.
type RunIndexEntry struct {
	RunID      string    `json:"runId"`
	RunRoot    string    `json:"runRoot"`
	StackName  string    `json:"stackName,omitempty"`
	Profile    string    `json:"profile,omitempty"`
	Status     string    `json:"status,omitempty"`
	StartedAt  string    `json:"startedAt,omitempty"`
	UpdatedAt  string    `json:"updatedAt,omitempty"`
	Totals     RunTotals `json:"totals,omitempty"`
	HasSummary bool      `json:"hasSummary"`
}
