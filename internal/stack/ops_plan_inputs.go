package stack

// OpsPlanInputs records read-only ops evidence consumed while compiling a stack plan.
type OpsPlanInputs struct {
	APIVersion      string                   `json:"apiVersion"`
	Kind            string                   `json:"kind"`
	TargetGraph     *OpsTargetGraphInput     `json:"targetGraph,omitempty"`
	FactEvidence    []OpsFactEvidenceInput   `json:"factEvidence,omitempty"`
	Locks           []OpsLockInput           `json:"locks,omitempty"`
	PolicyDecisions []OpsPolicyDecisionInput `json:"policyDecisions,omitempty"`
	Blockers        []OpsPlanBlocker         `json:"blockers,omitempty"`
	Summary         OpsPlanInputSummary      `json:"summary"`
}

type OpsTargetGraphInput struct {
	Path         string                  `json:"path,omitempty"`
	Name         string                  `json:"name"`
	SourceDigest string                  `json:"sourceDigest,omitempty"`
	GraphDigest  string                  `json:"graphDigest,omitempty"`
	Summary      OpsTargetGraphSummary   `json:"summary"`
	Selection    OpsTargetSelectionInput `json:"selection"`
}

type OpsTargetGraphSummary struct {
	APIVersion            string         `json:"apiVersion,omitempty"`
	Kind                  string         `json:"kind,omitempty"`
	Name                  string         `json:"name,omitempty"`
	TargetCount           int            `json:"targetCount"`
	GroupCount            int            `json:"groupCount,omitempty"`
	TransportCount        int            `json:"transportCount,omitempty"`
	PrivilegeProfileCount int            `json:"privilegeProfileCount,omitempty"`
	VariableLayerCount    int            `json:"variableLayerCount,omitempty"`
	TargetTypes           map[string]int `json:"targetTypes,omitempty"`
	TransportKinds        map[string]int `json:"transportKinds,omitempty"`
	GroupIDs              []string       `json:"groupIds,omitempty"`
	TargetIDs             []string       `json:"targetIds,omitempty"`
	SecretReferenceCount  int            `json:"secretReferenceCount,omitempty"`
	HostReachabilityRefs  []string       `json:"hostReachabilityRefs,omitempty"`
}

type OpsTargetSelectionInput struct {
	Selector           map[string]string `json:"selector,omitempty"`
	Groups             []string          `json:"groups,omitempty"`
	MatchedTargetIDs   []string          `json:"matchedTargetIds"`
	OmittedTargetIDs   []string          `json:"omittedTargetIds,omitempty"`
	SelectorMatchedIDs []string          `json:"selectorMatchedIds,omitempty"`
	Limited            bool              `json:"limited"`
	BeforeLimitCount   int               `json:"beforeLimitCount"`
	AfterLimitCount    int               `json:"afterLimitCount"`
	ConflictCount      int               `json:"conflictCount,omitempty"`
}

type OpsFactEvidenceInput struct {
	Source    string                 `json:"source"`
	Kind      string                 `json:"kind"`
	Digest    string                 `json:"digest,omitempty"`
	GraphName string                 `json:"graphName,omitempty"`
	Targets   []OpsFactTargetInput   `json:"targets,omitempty"`
	Freshness *OpsFactFreshnessInput `json:"freshness,omitempty"`
	Summary   OpsFactEvidenceSummary `json:"summary"`
}

type OpsFactTargetInput struct {
	TargetID      string `json:"targetId"`
	TargetType    string `json:"targetType,omitempty"`
	TransportKind string `json:"transportKind,omitempty"`
	Status        string `json:"status,omitempty"`
	Source        string `json:"source,omitempty"`
	SnapshotKind  string `json:"snapshotKind,omitempty"`
	Digest        string `json:"digest,omitempty"`
}

type OpsFactFreshnessInput struct {
	HostDecisions   int `json:"hostDecisions,omitempty"`
	K8sObservations int `json:"k8sObservations,omitempty"`
	Blocked         int `json:"blocked,omitempty"`
	Failed          int `json:"failed,omitempty"`
}

type OpsFactEvidenceSummary struct {
	Selected  int `json:"selected,omitempty"`
	Targets   int `json:"targets,omitempty"`
	Snapshots int `json:"snapshots,omitempty"`
	Collected int `json:"collected,omitempty"`
	Cached    int `json:"cached,omitempty"`
	Blocked   int `json:"blocked,omitempty"`
	Failed    int `json:"failed,omitempty"`
	Skipped   int `json:"skipped,omitempty"`
}

type OpsLockInput struct {
	Source     string `json:"source,omitempty"`
	Scope      string `json:"scope"`
	Found      bool   `json:"found"`
	TargetID   string `json:"targetId,omitempty"`
	Status     string `json:"status,omitempty"`
	Holder     string `json:"holder,omitempty"`
	Operation  string `json:"operation,omitempty"`
	AcquiredAt string `json:"acquiredAt,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
}

type OpsPolicyDecisionInput struct {
	Source    string   `json:"source,omitempty"`
	Digest    string   `json:"digest,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Decision  string   `json:"decision"`
	Reason    string   `json:"reason,omitempty"`
	Operation string   `json:"operation,omitempty"`
	Adapter   string   `json:"adapter,omitempty"`
	TargetID  string   `json:"targetId,omitempty"`
	Mutating  bool     `json:"mutating,omitempty"`
	Unsafe    bool     `json:"unsafe,omitempty"`
	Required  []string `json:"required,omitempty"`
}

type OpsPlanBlocker struct {
	Code     string `json:"code"`
	Severity string `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	TargetID string `json:"targetId,omitempty"`
	Scope    string `json:"scope,omitempty"`
	Reason   string `json:"reason"`
}

type OpsPlanInputSummary struct {
	TargetCount     int `json:"targetCount,omitempty"`
	SelectedTargets int `json:"selectedTargets,omitempty"`
	FactEvidence    int `json:"factEvidence,omitempty"`
	FactSnapshots   int `json:"factSnapshots,omitempty"`
	FactBlocked     int `json:"factBlocked,omitempty"`
	FactFailed      int `json:"factFailed,omitempty"`
	Locks           int `json:"locks,omitempty"`
	LockHeld        int `json:"lockHeld,omitempty"`
	LockStale       int `json:"lockStale,omitempty"`
	PolicyDecisions int `json:"policyDecisions,omitempty"`
	PolicyBlocked   int `json:"policyBlocked,omitempty"`
	Blockers        int `json:"blockers,omitempty"`
}
