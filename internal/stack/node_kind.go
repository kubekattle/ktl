package stack

import "strings"

const (
	NodeKindHelm              = "release.helm"
	NodeKindAction            = "action.script"
	NodeKindActionPlugin      = "action.plugin"
	NodeKindDBRestorePoint    = "db.restore-point"
	NodeKindDBSchemaExpand    = "db.schema-expand"
	NodeKindDBBackfill        = "db.backfill"
	NodeKindDBVerify          = "db.verify"
	NodeKindDBCutover         = "db.cutover"
	NodeKindDBSchemaContract  = "db.schema-contract"
	NodeKindHostCommandRun    = "host.command.run"
	NodeKindK8sClusterInspect = "k8s.cluster.inspect"
	NodeKindK8sCertInspect    = "k8s.cert.inspect"
	NodeKindK8sCertRenew      = "k8s.cert.renew"
	NodeKindK8sClusterVerify  = "k8s.cluster.verify"
)

func normalizeNodeKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" || kind == "release" || kind == "helm" {
		return NodeKindHelm
	}
	return kind
}

func isHelmNode(n *ResolvedRelease) bool {
	if n == nil {
		return false
	}
	return normalizeNodeKind(n.Kind) == NodeKindHelm
}

func nodeStepSucceeded(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "skipped":
		return true
	default:
		return false
	}
}
