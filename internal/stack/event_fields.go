package stack

import "strings"

const runEventFieldsVersion = 1

const (
	fieldVersionKey = "v"
)

type NodeMetaFields struct {
	Cluster          string
	Namespace        string
	Name             string
	ExecutionGroup   int
	ParallelismGroup string
	PrimaryKind      string
	Critical         bool
}

func (f NodeMetaFields) Map() map[string]any {
	return map[string]any{
		fieldVersionKey:    runEventFieldsVersion,
		"cluster":          strings.TrimSpace(f.Cluster),
		"namespace":        strings.TrimSpace(f.Namespace),
		"name":             strings.TrimSpace(f.Name),
		"executionGroup":   f.ExecutionGroup,
		"parallelismGroup": strings.TrimSpace(f.ParallelismGroup),
		"primaryKind":      strings.TrimSpace(f.PrimaryKind),
		"critical":         f.Critical,
	}
}

type BudgetWaitFields struct {
	BudgetType string
	BudgetKey  string
	Limit      int64
	Used       int64
}

func (f BudgetWaitFields) Map() map[string]any {
	return map[string]any{
		fieldVersionKey: runEventFieldsVersion,
		"budgetType":    strings.TrimSpace(f.BudgetType),
		"budgetKey":     strings.TrimSpace(f.BudgetKey),
		"limit":         f.Limit,
		"used":          f.Used,
	}
}

type ConcurrencyFields struct {
	From     int
	To       int
	Reason   string
	Class    string
	Window   int
	FailRate float64
}

func (f ConcurrencyFields) Map() map[string]any {
	out := map[string]any{
		fieldVersionKey: runEventFieldsVersion,
		"from":          f.From,
		"to":            f.To,
		"reason":        strings.TrimSpace(f.Reason),
		"window":        f.Window,
		"failRate":      f.FailRate,
	}
	if strings.TrimSpace(f.Class) != "" {
		out["class"] = strings.TrimSpace(f.Class)
	}
	return out
}

type PhaseFields struct {
	Phase   string
	Status  string
	Message string
}

func (f PhaseFields) Map() map[string]any {
	out := map[string]any{
		fieldVersionKey: runEventFieldsVersion,
		"phase":         strings.TrimSpace(f.Phase),
	}
	if strings.TrimSpace(f.Status) != "" {
		out["status"] = strings.TrimSpace(f.Status)
	}
	if strings.TrimSpace(f.Message) != "" {
		out["message"] = strings.TrimSpace(f.Message)
	}
	return out
}

type RetryScheduledFields struct {
	Backoff string
}

func (f RetryScheduledFields) Map() map[string]any {
	return map[string]any{
		fieldVersionKey: runEventFieldsVersion,
		"backoff":       strings.TrimSpace(f.Backoff),
	}
}
