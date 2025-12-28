package stack

import "fmt"

func releaseRoleOrder(role string) int {
	switch role {
	case "namespace":
		return 10
	case "crd":
		return 20
	case "rbac":
		return 30
	case "webhook":
		return 40
	case "workload":
		return 50
	case "":
		return 90
	default:
		return 80
	}
}

func releaseReadyKey(n *ResolvedRelease) string {
	if n == nil {
		return ""
	}
	wave := n.Wave
	if wave < 0 {
		// Keep ordering stable even if users set negative values.
		wave = 0
	}
	return fmt.Sprintf("%08d:%02d:%s", wave, releaseRoleOrder(n.InferredRole), n.ID)
}
