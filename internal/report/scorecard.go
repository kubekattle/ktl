// scorecard.go models the metrics/threshold scoring used by 'ktl diag report'.
package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/drift"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/podsecurity"
	"github.com/example/ktl/internal/quota"
	_ "modernc.org/sqlite"
)

// Scorecard captures the high-level health posture that powers the HTML/CLI score widgets.
type Scorecard struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Average     float64       `json:"average"`
	Checks      []ScoreCheck  `json:"checks"`
	Insights    ScoreInsights `json:"insights"`
}

// ScoreInsights provides contextual data for UI enhancements.
type ScoreInsights struct {
	PodSecurityViolations map[string]int `json:"podSecurityViolations,omitempty"`
	BudgetWidgets         []BudgetWidget `json:"budgetWidgets,omitempty"`
}

type resourceKey string

const (
	resourcePods resourceKey = "pods"
	resourceCPU  resourceKey = "cpu"
	resourceMem  resourceKey = "memory"
	resourcePVC  resourceKey = "pvcs"
)

// BudgetWidget feeds the sidebar donut charts.
type BudgetWidget struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Usage        float64  `json:"usage"`
	Namespaces   []string `json:"namespaces,omitempty"`
	NamespaceCSV string   `json:"-"`
}

// ScoreCheck represents a single posture dimension (pod security, quota headroom, etc).
type ScoreCheck struct {
	Key          string      `json:"key"`
	Name         string      `json:"name"`
	Score        float64     `json:"score"`
	BudgetUsed   float64     `json:"budgetUsed"`
	Delta        float64     `json:"delta"`
	Trend        []float64   `json:"trend,omitempty"`
	Status       ScoreStatus `json:"status"`
	Summary      string      `json:"summary"`
	Details      []string    `json:"details,omitempty"`
	Command      string      `json:"command"`
	TrendEncoded string      `json:"-"`
}

// ScoreStatus communicates whether a check is healthy, warning, or failed.
type ScoreStatus string

const (
	// ScoreStatusPass indicates the score is comfortably above the warning threshold.
	ScoreStatusPass ScoreStatus = "pass"
	// ScoreStatusWarn indicates the score is degrading but still above the failure threshold.
	ScoreStatusWarn ScoreStatus = "warn"
	// ScoreStatusFail indicates the score is below the failure threshold.
	ScoreStatusFail ScoreStatus = "fail"
	// ScoreStatusUnknown indicates the score could not be computed.
	ScoreStatusUnknown ScoreStatus = "unknown"
)

// NotificationPayload is emitted when thresholds trip.
type NotificationPayload struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	Threshold   float64      `json:"threshold"`
	Average     float64      `json:"average"`
	Checks      []ScoreCheck `json:"checks"`
}

func buildScorecard(ctx context.Context, client *kube.Client, namespaces []string, sections []namespaceSection) Scorecard {
	card := Scorecard{
		GeneratedAt: time.Now().UTC(),
	}
	var checks []ScoreCheck
	insights := ScoreInsights{}

	if check, violators, err := scorePodSecurity(ctx, client, namespaces); err != nil {
		checks = append(checks, errorCheck("podsecurity", "Pod Security", commandFor("ktl diag podsecurity", namespaces), err))
	} else {
		checks = append(checks, check)
		insights.PodSecurityViolations = violators
	}

	if check, widgets, err := scoreQuota(ctx, client, namespaces); err != nil {
		checks = append(checks, errorCheck("quotas", "Quota Headroom", commandFor("ktl diag quotas", namespaces), err))
	} else {
		checks = append(checks, check)
		for i := range widgets {
			widgets[i].NamespaceCSV = strings.Join(widgets[i].Namespaces, ",")
		}
		insights.BudgetWidgets = widgets
	}

	if check, err := scoreDrift(ctx, client, namespaces); err != nil {
		checks = append(checks, errorCheck("drift", "Rollout Drift", commandFor("ktl logs drift watch", namespaces), err))
	} else {
		checks = append(checks, check)
	}

	checks = append(checks, scoreSLOBurn(sections, namespaces))

	card.Checks = checks
	card.Average = averageScore(checks)
	card.Insights = insights
	enrichScorecardHistory(&card)
	return card
}

func (sc Scorecard) Breaches(threshold float64) []ScoreCheck {
	if threshold <= 0 {
		return nil
	}
	var failing []ScoreCheck
	for _, check := range sc.Checks {
		if check.Status == ScoreStatusUnknown {
			continue
		}
		if check.Score < threshold {
			failing = append(failing, check)
		}
	}
	return failing
}

func (sc Scorecard) NotificationPayload(threshold float64, checks []ScoreCheck) NotificationPayload {
	if len(checks) == 0 {
		checks = sc.Breaches(threshold)
	}
	return NotificationPayload{
		GeneratedAt: sc.GeneratedAt,
		Threshold:   threshold,
		Average:     sc.Average,
		Checks:      checks,
	}
}

func averageScore(checks []ScoreCheck) float64 {
	var total float64
	var count float64
	for _, check := range checks {
		if check.Status == ScoreStatusUnknown {
			continue
		}
		total += check.Score
		count++
	}
	if count == 0 {
		return 0
	}
	return math.Round((total/count)*10) / 10
}

func scorePodSecurity(ctx context.Context, client *kube.Client, namespaces []string) (ScoreCheck, map[string]int, error) {
	opts := podsecurity.Options{
		Namespaces:       namespaces,
		DefaultNamespace: client.Namespace,
	}
	summaries, err := podsecurity.Collect(ctx, client.Clientset, opts)
	if err != nil {
		return ScoreCheck{}, nil, err
	}
	total := len(summaries)
	if total == 0 {
		return ScoreCheck{
			Key:        "podsecurity",
			Name:       "Pod Security",
			Score:      100,
			BudgetUsed: 0,
			Status:     ScoreStatusPass,
			Summary:    "No namespaces matched the selection",
			Command:    commandFor("ktl diag podsecurity", namespaces),
		}, nil, nil
	}
	var restricted int
	var findingsTotal int
	var violators []string
	violationMap := make(map[string]int)
	for _, summary := range summaries {
		if strings.EqualFold(summary.Labels.Enforce, "restricted") {
			restricted++
		}
		if len(summary.Findings) > 0 {
			violators = append(violators, fmt.Sprintf("%s (%d findings)", summary.Namespace, len(summary.Findings)))
			findingsTotal += len(summary.Findings)
			violationMap[summary.Namespace] = len(summary.Findings)
		}
	}
	base := float64(restricted) / float64(total) * 100
	penalty := math.Min(40, float64(findingsTotal)*2)
	score := clampScore(base - penalty)
	return ScoreCheck{
		Key:        "podsecurity",
		Name:       "Pod Security",
		Score:      score,
		BudgetUsed: 100 - score,
		Status:     statusForScore(score),
		Summary:    fmt.Sprintf("%d/%d namespaces enforce restricted (%d findings)", restricted, total, findingsTotal),
		Details:    capStrings(violators, 4),
		Command:    commandFor("ktl diag podsecurity", namespaces),
	}, violationMap, nil
}

func scoreQuota(ctx context.Context, client *kube.Client, namespaces []string) (ScoreCheck, []BudgetWidget, error) {
	opts := quota.Options{
		Namespaces:       namespaces,
		DefaultNamespace: client.Namespace,
	}
	summaries, err := quota.Collect(ctx, client.Clientset, opts)
	if err != nil {
		return ScoreCheck{}, nil, err
	}
	var ratios []float64
	var hot []string
	type agg struct {
		sum float64
		n   float64
		hot []string
	}
	resources := map[resourceKey]*agg{
		resourcePods: {},
		resourceCPU:  {},
		resourceMem:  {},
		resourcePVC:  {},
	}
	for _, summary := range summaries {
		resourceMap := map[resourceKey]quota.Metric{
			resourcePods: summary.Pods,
			resourceCPU:  summary.CPU,
			resourceMem:  summary.Memory,
			resourcePVC:  summary.PVCs,
		}
		for name, metric := range resourceMap {
			if !metric.HasLimit || metric.Limit == 0 {
				continue
			}
			ratio := float64(metric.Used) / float64(metric.Limit)
			if ratio < 0 {
				ratio = 0
			}
			ratios = append(ratios, ratio)
			if ratio >= 0.9 {
				hot = append(hot, fmt.Sprintf("%s %s at %.0f%%", summary.Namespace, resourceMapName(name), ratio*100))
				res := resources[name]
				if res != nil && len(res.hot) < 6 {
					res.hot = append(res.hot, summary.Namespace)
				}
			}
			res := resources[name]
			if res != nil {
				res.sum += ratio
				res.n++
			}
		}
	}
	if len(ratios) == 0 {
		return ScoreCheck{
			Key:        "quotas",
			Name:       "Quota Headroom",
			Score:      100,
			BudgetUsed: 0,
			Status:     ScoreStatusPass,
			Summary:    "No quota limits detected",
			Command:    commandFor("ktl diag quotas", namespaces),
		}, nil, nil
	}
	var sum float64
	for _, ratio := range ratios {
		sum += ratio
	}
	avgUsage := sum / float64(len(ratios))
	budget := math.Min(100, avgUsage*100)
	score := clampScore(100 - budget)
	labels := map[resourceKey]string{
		resourcePods: "Pods",
		resourceCPU:  "CPU Requests",
		resourceMem:  "Memory Requests",
		resourcePVC:  "PVCs",
	}
	var widgets []BudgetWidget
	for key, label := range labels {
		res := resources[key]
		if res == nil || res.n == 0 {
			continue
		}
		usage := (res.sum / res.n) * 100
		widgets = append(widgets, BudgetWidget{
			Key:        string(key),
			Label:      label,
			Usage:      clampScore(usage),
			Namespaces: uniqueStrings(res.hot),
		})
	}
	sort.Slice(widgets, func(i, j int) bool { return widgets[i].Usage > widgets[j].Usage })
	return ScoreCheck{
		Key:        "quotas",
		Name:       "Quota Headroom",
		Score:      score,
		BudgetUsed: budget,
		Status:     statusForScore(score),
		Summary:    fmt.Sprintf("Avg consumption %.0f%% across %d namespaces", avgUsage*100, len(summaries)),
		Details:    capStrings(hot, 4),
		Command:    commandFor("ktl diag quotas", namespaces),
	}, widgets, nil
}

func scoreDrift(ctx context.Context, client *kube.Client, namespaces []string) (ScoreCheck, error) {
	targets := namespaces
	if len(targets) == 0 {
		targets = []string{client.Namespace}
	}
	collector := drift.NewCollector(client.Clientset, targets, 2)
	snapshot, err := collector.Snapshot(ctx)
	if err != nil {
		return ScoreCheck{}, err
	}
	statePath := filepath.Join(scorecardStateDir(), "drift-state.json")
	prev, _ := loadSnapshot(statePath)
	diff := drift.DiffSnapshots(prev, &snapshot)
	changeCount := len(diff.Added) + len(diff.Removed) + len(diff.Changed)
	var details []string
	for _, change := range diff.Changed {
		if len(details) >= 3 {
			break
		}
		details = append(details, fmt.Sprintf("%s/%s: %s", change.Namespace, change.Name, strings.Join(change.Reasons, "; ")))
	}
	summary := fmt.Sprintf("%d added · %d removed · %d changed", len(diff.Added), len(diff.Removed), len(diff.Changed))
	if prev == nil {
		summary = "Baseline snapshot captured"
	}
	penalty := math.Min(100, float64(changeCount)*5)
	score := clampScore(100 - penalty)
	if err := saveSnapshot(statePath, snapshot); err != nil {
		details = append(details, fmt.Sprintf("state persistence failed: %v", err))
	}
	return ScoreCheck{
		Key:        "drift",
		Name:       "Rollout Drift",
		Score:      score,
		BudgetUsed: 100 - score,
		Status:     statusForScore(score),
		Summary:    summary,
		Details:    details,
		Command:    commandFor("ktl logs drift watch --snapshot", namespaces),
	}, nil
}

func scoreSLOBurn(sections []namespaceSection, namespaces []string) ScoreCheck {
	var totalPods int
	var impacted int
	var offenders []string
	for _, section := range sections {
		for _, pod := range section.Pods {
			totalPods++
			impactedPod := pod.ReadyContainers < pod.TotalContainers
			if !impactedPod {
				for _, c := range pod.Containers {
					if c.Restarts >= 5 {
						impactedPod = true
						break
					}
				}
			}
			if impactedPod {
				impacted++
				if len(offenders) < 4 {
					offenders = append(offenders, fmt.Sprintf("%s/%s (%s)", section.Name, pod.Name, pod.ReadyCount))
				}
			}
		}
	}
	if totalPods == 0 {
		return ScoreCheck{
			Key:        "slo",
			Name:       "SLO Burn",
			Score:      100,
			BudgetUsed: 0,
			Status:     ScoreStatusPass,
			Summary:    "No pods discovered in the selection",
			Command:    commandFor("ktl logs drift watch --events", namespaces),
		}
	}
	burn := float64(impacted) / float64(totalPods)
	budget := math.Min(100, burn*100)
	score := clampScore(100 - budget*1.5)
	return ScoreCheck{
		Key:        "slo",
		Name:       "SLO Burn",
		Score:      score,
		BudgetUsed: 100 - score,
		Status:     statusForScore(score),
		Summary:    fmt.Sprintf("%d/%d pods outside SLO window (%.1f%% burn)", impacted, totalPods, burn*100),
		Details:    capStrings(offenders, 4),
		Command:    commandFor("ktl logs drift watch --events", namespaces),
	}
}

func resourceMapName(key resourceKey) string {
	switch key {
	case resourcePods:
		return "pods"
	case resourceCPU:
		return "cpu"
	case resourceMem:
		return "memory"
	case resourcePVC:
		return "pvcs"
	default:
		return string(key)
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

const trendHistoryDepth = 6

func enrichScorecardHistory(card *Scorecard) {
	if card == nil || len(card.Checks) == 0 {
		return
	}
	db, err := openHistoryDB()
	if err != nil {
		// history is optional; ignore errors (likely first run)
		return
	}
	defer db.Close()
	rows, err := db.Query(`SELECT c.key, c.score
		FROM scorecard_checks c
		JOIN scorecard_snapshots s ON c.snapshot_id = s.id
		ORDER BY s.generated_at DESC`)
	if err != nil {
		return
	}
	defer rows.Close()
	history := make(map[string][]float64)
	for rows.Next() {
		var key string
		var score float64
		if err := rows.Scan(&key, &score); err != nil {
			return
		}
		if len(history[key]) >= trendHistoryDepth {
			continue
		}
		history[key] = append(history[key], score)
	}
	for i := range card.Checks {
		values := history[card.Checks[i].Key]
		if len(values) > 0 {
			card.Checks[i].Delta = round1(card.Checks[i].Score - values[0])
		}
		trend := make([]float64, 0, len(values)+1)
		for j := len(values) - 1; j >= 0; j-- {
			trend = append(trend, values[j])
		}
		trend = append(trend, card.Checks[i].Score)
		if len(trend) > trendHistoryDepth+1 {
			trend = trend[len(trend)-(trendHistoryDepth+1):]
		}
		card.Checks[i].Trend = trend
		card.Checks[i].TrendEncoded = encodeTrend(trend)
	}
}

func round1(val float64) float64 {
	return math.Round(val*10) / 10
}

func encodeTrend(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprintf("%.2f", v)
	}
	return strings.Join(out, ",")
}

func clampScore(val float64) float64 {
	switch {
	case math.IsNaN(val):
		return 0
	case val < 0:
		return 0
	case val > 100:
		return 100
	default:
		return val
	}
}

func statusForScore(score float64) ScoreStatus {
	switch {
	case score >= 90:
		return ScoreStatusPass
	case score >= 75:
		return ScoreStatusWarn
	case score >= 0:
		return ScoreStatusFail
	default:
		return ScoreStatusUnknown
	}
}

func errorCheck(key, name, cmd string, err error) ScoreCheck {
	return ScoreCheck{
		Key:        key,
		Name:       name,
		Score:      0,
		BudgetUsed: 100,
		Status:     ScoreStatusUnknown,
		Summary:    fmt.Sprintf("unable to compute: %v", err),
		Command:    cmd,
	}
}

func capStrings(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	out := append([]string{}, values[:max]...)
	out = append(out, fmt.Sprintf("… +%d more", len(values)-max))
	return out
}

func commandFor(base string, namespaces []string) string {
	if len(namespaces) == 0 {
		return base
	}
	segments := []string{base}
	for _, ns := range namespaces {
		segments = append(segments, fmt.Sprintf("-n %s", ns))
		if len(segments) > 6 {
			segments = append(segments, "…")
			break
		}
	}
	return strings.Join(segments, " ")
}

func scorecardStateDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "ktl", "scorecard")
}

func loadSnapshot(path string) (*drift.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap drift.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func saveSnapshot(path string, snapshot drift.Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func scorecardHistoryPath() string {
	return filepath.Join(scorecardStateDir(), "history.db")
}

// AppendScorecardHistory saves the provided scorecard into the local SQLite history.
func AppendScorecardHistory(card Scorecard) error {
	if card.GeneratedAt.IsZero() {
		card.GeneratedAt = time.Now().UTC()
	}
	db, err := openHistoryDB()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("INSERT INTO scorecard_snapshots(generated_at, average) VALUES(?, ?)", card.GeneratedAt.Format(time.RFC3339Nano), card.Average)
	if err != nil {
		return err
	}
	snapshotID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, check := range card.Checks {
		detailsJSON, _ := json.Marshal(check.Details)
		if _, err := tx.Exec(`INSERT INTO scorecard_checks(snapshot_id, key, name, score, budget_used, status, summary, command, details)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshotID, check.Key, check.Name, check.Score, check.BudgetUsed, string(check.Status), check.Summary, check.Command, string(detailsJSON)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM scorecard_snapshots WHERE id NOT IN (
		SELECT id FROM scorecard_snapshots ORDER BY generated_at DESC LIMIT 200
	)`); err != nil {
		return err
	}
	return tx.Commit()
}

// TrendEntry represents a historical snapshot loaded from SQLite.
type TrendEntry struct {
	GeneratedAt time.Time
	Average     float64
	Checks      []ScoreCheck
}

// LoadScorecardTrend returns scorecard snapshots within the optional day window.
func LoadScorecardTrend(days int) ([]TrendEntry, error) {
	db, err := openHistoryDB()
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no scorecard history recorded yet (run ktl diag report first)")
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var rows *sql.Rows
	cutoff := ""
	query := `SELECT id, generated_at, average FROM scorecard_snapshots ORDER BY generated_at DESC`
	if days > 0 {
		cutoff = time.Now().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339Nano)
		query = `SELECT id, generated_at, average FROM scorecard_snapshots WHERE generated_at >= ? ORDER BY generated_at DESC`
		rows, err = db.Query(query, cutoff)
	} else {
		rows, err = db.Query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var trend []TrendEntry
	for rows.Next() {
		var id int64
		var ts string
		var avg float64
		if err := rows.Scan(&id, &ts, &avg); err != nil {
			return nil, err
		}
		parsed, _ := time.Parse(time.RFC3339Nano, ts)
		checkRows, err := db.Query(`SELECT key, name, score, budget_used, status, summary, command, details FROM scorecard_checks WHERE snapshot_id = ? ORDER BY key`, id)
		if err != nil {
			return nil, err
		}
		var checks []ScoreCheck
		for checkRows.Next() {
			var check ScoreCheck
			var status string
			var details string
			if err := checkRows.Scan(&check.Key, &check.Name, &check.Score, &check.BudgetUsed, &status, &check.Summary, &check.Command, &details); err != nil {
				checkRows.Close()
				return nil, err
			}
			check.Status = ScoreStatus(status)
			if details != "" {
				_ = json.Unmarshal([]byte(details), &check.Details)
			}
			checks = append(checks, check)
		}
		checkRows.Close()
		trend = append(trend, TrendEntry{
			GeneratedAt: parsed,
			Average:     avg,
			Checks:      checks,
		})
	}
	return trend, nil
}

func openHistoryDB() (*sql.DB, error) {
	path := scorecardHistoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureHistorySchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureHistorySchema(db *sql.DB) error {
	const snapshots = `
CREATE TABLE IF NOT EXISTS scorecard_snapshots(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	generated_at TEXT NOT NULL,
	average REAL NOT NULL
);`
	const checks = `
CREATE TABLE IF NOT EXISTS scorecard_checks(
	snapshot_id INTEGER NOT NULL,
	key TEXT NOT NULL,
	name TEXT NOT NULL,
	score REAL NOT NULL,
	budget_used REAL NOT NULL,
	status TEXT NOT NULL,
	summary TEXT,
	command TEXT,
	details TEXT,
	FOREIGN KEY(snapshot_id) REFERENCES scorecard_snapshots(id) ON DELETE CASCADE
);`
	if _, err := db.Exec(snapshots); err != nil {
		return err
	}
	if _, err := db.Exec(checks); err != nil {
		return err
	}
	return nil
}
