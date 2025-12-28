package stack

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type RunAudit struct {
	APIVersion    string `json:"apiVersion"`
	RunID         string `json:"runId"`
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	CompletedAt   string `json:"completedAt,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
	Host          string `json:"host,omitempty"`
	PID           int    `json:"pid,omitempty"`
	CIRunURL      string `json:"ciRunUrl,omitempty"`
	GitAuthor     string `json:"gitAuthor,omitempty"`
	Kubeconfig    string `json:"kubeconfig,omitempty"`
	KubeContext   string `json:"kubeContext,omitempty"`
	StatePath     string `json:"statePath,omitempty"`
	FollowCommand string `json:"followCommand,omitempty"`
	RunDigest     string `json:"runDigest,omitempty"`

	Integrity       RunIntegrity     `json:"integrity"`
	Summary         *RunSummary      `json:"summary,omitempty"`
	FailureClusters []FailureCluster `json:"failureClusters,omitempty"`

	Plan   *RunPlan   `json:"plan,omitempty"`
	Events []RunEvent `json:"events,omitempty"`
}

type RunIntegrity struct {
	EventsOK         bool   `json:"eventsOk"`
	EventsError      string `json:"eventsError,omitempty"`
	LastEventDigest  string `json:"lastEventDigest,omitempty"`
	StoredLastDigest string `json:"storedLastDigest,omitempty"`

	RunDigestOK       bool   `json:"runDigestOk"`
	RunDigestExpected string `json:"runDigestExpected,omitempty"`
	RunDigestStored   string `json:"runDigestStored,omitempty"`
	RunDigestError    string `json:"runDigestError,omitempty"`
}

type FailureCluster struct {
	ErrorClass     string   `json:"errorClass"`
	ErrorDigest    string   `json:"errorDigest"`
	FailedEvents   int      `json:"failedEvents"`
	AffectedNodes  int      `json:"affectedNodes"`
	ExampleNodeIDs []string `json:"exampleNodeIds,omitempty"`
}

type RunAuditOptions struct {
	RootDir       string
	RunID         string
	Verify        bool
	EventsLimit   int
	IncludePlan   bool
	IncludeEvents bool
}

func GetRunAudit(ctx context.Context, opts RunAuditOptions) (*RunAudit, error) {
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		root = "."
	}
	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		var err error
		runID, err = LoadMostRecentRun(root)
		if err != nil {
			return nil, err
		}
	}

	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	s, err := openStackStateStore(root, true)
	if err != nil {
		return nil, fmt.Errorf("open stack state %s: %w", statePath, err)
	}
	defer s.Close()

	var (
		status         string
		createdAtNS    int64
		updatedAtNS    int64
		completedAtNS  int64
		createdBy      string
		host           string
		pid            int
		ciRunURL       string
		gitAuthor      string
		kubeconfig     string
		kubeContext    string
		planJSON       string
		summaryJSON    string
		storedLastHash string
		storedRunHash  string
	)
	err = s.db.QueryRowContext(ctx, `
SELECT status, created_at_ns, updated_at_ns, completed_at_ns, created_by, host, pid, ci_run_url, git_author, kubeconfig, kube_context, plan_json, summary_json, last_event_digest, run_digest
FROM ktl_stack_runs
WHERE run_id = ?
`, runID).Scan(&status, &createdAtNS, &updatedAtNS, &completedAtNS, &createdBy, &host, &pid, &ciRunURL, &gitAuthor, &kubeconfig, &kubeContext, &planJSON, &summaryJSON, &storedLastHash, &storedRunHash)
	if err != nil {
		return nil, err
	}

	var summary RunSummary
	if err := json.Unmarshal([]byte(summaryJSON), &summary); err != nil {
		return nil, fmt.Errorf("parse run %s summary: %w", runID, err)
	}

	a := &RunAudit{
		APIVersion:    "ktl.dev/stack-audit/v1",
		RunID:         runID,
		Status:        strings.TrimSpace(status),
		CreatedAt:     time.Unix(0, createdAtNS).UTC().Format(time.RFC3339Nano),
		UpdatedAt:     time.Unix(0, updatedAtNS).UTC().Format(time.RFC3339Nano),
		CreatedBy:     strings.TrimSpace(createdBy),
		Host:          strings.TrimSpace(host),
		PID:           pid,
		CIRunURL:      strings.TrimSpace(ciRunURL),
		GitAuthor:     strings.TrimSpace(gitAuthor),
		Kubeconfig:    strings.TrimSpace(kubeconfig),
		KubeContext:   strings.TrimSpace(kubeContext),
		StatePath:     strings.TrimSpace(s.path),
		FollowCommand: buildStackStatusFollowCommand(root, runID),
		RunDigest:     strings.TrimSpace(storedRunHash),
		Integrity:     RunIntegrity{EventsOK: true, RunDigestOK: true},
		Summary:       &summary,
	}
	if completedAtNS > 0 {
		a.CompletedAt = time.Unix(0, completedAtNS).UTC().Format(time.RFC3339Nano)
	}

	clusters, err := loadFailureClusters(ctx, s, runID, 5)
	if err == nil {
		a.FailureClusters = clusters
	}

	if opts.IncludePlan {
		var rp RunPlan
		if err := json.Unmarshal([]byte(planJSON), &rp); err == nil {
			a.Plan = &rp
		}
	}
	if opts.IncludeEvents {
		limit := opts.EventsLimit
		if limit == 0 {
			limit = 1000
		}
		if events, err := s.ListEvents(ctx, runID, limit); err == nil {
			a.Events = events
		}
	}

	if !opts.Verify {
		return a, nil
	}

	// Verify hash chain.
	if err := s.VerifyEventsIntegrity(ctx, runID); err != nil {
		a.Integrity.EventsOK = false
		a.Integrity.EventsError = err.Error()
	}

	var lastDigest string
	row := s.db.QueryRowContext(ctx, `SELECT digest FROM ktl_stack_events WHERE run_id = ? ORDER BY id DESC LIMIT 1`, runID)
	switch err := row.Scan(&lastDigest); err {
	case nil:
		lastDigest = strings.TrimSpace(lastDigest)
	case sql.ErrNoRows:
		lastDigest = ""
	default:
		a.Integrity.EventsOK = false
		a.Integrity.EventsError = fmt.Sprintf("read last event digest: %v", err)
	}
	a.Integrity.LastEventDigest = lastDigest
	a.Integrity.StoredLastDigest = strings.TrimSpace(storedLastHash)
	if a.Integrity.StoredLastDigest != "" && lastDigest != "" && a.Integrity.StoredLastDigest != lastDigest {
		a.Integrity.EventsOK = false
		if a.Integrity.EventsError == "" {
			a.Integrity.EventsError = "stored last_event_digest does not match last event digest"
		}
	}

	// Verify run digest if present.
	lastForDigest := lastDigest
	if strings.TrimSpace(lastForDigest) == "" {
		lastForDigest = strings.TrimSpace(storedLastHash)
	}
	expected := computeRunDigest(planJSON, summaryJSON, lastForDigest)
	a.Integrity.RunDigestExpected = expected
	a.Integrity.RunDigestStored = strings.TrimSpace(storedRunHash)
	if a.Integrity.RunDigestStored == "" {
		a.Integrity.RunDigestOK = false
		a.Integrity.RunDigestError = "missing run_digest (run may predate digest support)"
	} else if a.Integrity.RunDigestStored != expected {
		a.Integrity.RunDigestOK = false
		a.Integrity.RunDigestError = "run_digest mismatch"
	}

	return a, nil
}

func buildStackStatusFollowCommand(root string, runID string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ""
	}
	if root == "." {
		return "ktl stack status --run-id " + runID + " --follow"
	}
	return "ktl stack --root " + root + " status --run-id " + runID + " --follow"
}

func loadFailureClusters(ctx context.Context, s *stackStateStore, runID string, limit int) ([]FailureCluster, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("state store not initialized")
	}
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT error_class, error_digest, COUNT(*) AS failed_events, COUNT(DISTINCT node_id) AS affected_nodes
FROM ktl_stack_events
WHERE run_id = ? AND type = 'NODE_FAILED' AND node_id != '' AND error_digest != ''
GROUP BY error_class, error_digest
ORDER BY affected_nodes DESC, failed_events DESC
LIMIT ?
`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FailureCluster
	for rows.Next() {
		var c FailureCluster
		if err := rows.Scan(&c.ErrorClass, &c.ErrorDigest, &c.FailedEvents, &c.AffectedNodes); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		rows2, err := s.db.QueryContext(ctx, `
SELECT DISTINCT node_id
FROM ktl_stack_events
WHERE run_id = ? AND type = 'NODE_FAILED' AND error_digest = ?
ORDER BY node_id ASC
LIMIT 5
`, runID, out[i].ErrorDigest)
		if err != nil {
			continue
		}
		var ids []string
		for rows2.Next() {
			var id string
			if err := rows2.Scan(&id); err != nil {
				break
			}
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
		_ = rows2.Close()
		out[i].ExampleNodeIDs = ids
	}
	return out, nil
}
