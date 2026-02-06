package verify

import (
	"strings"
)

func buildEvidence(obj map[string]any, fieldPath string) map[string]any {
	if obj == nil {
		return nil
	}
	kind := strings.TrimSpace(toString(obj["kind"]))
	ns := strings.TrimSpace(toString(toMap(obj["metadata"])["namespace"]))
	name := strings.TrimSpace(toString(toMap(obj["metadata"])["name"]))

	out := map[string]any{}
	if kind != "" {
		out["kind"] = kind
	}
	if ns != "" {
		out["namespace"] = ns
	}
	if name != "" {
		out["name"] = name
	}
	if strings.TrimSpace(fieldPath) != "" {
		out["fieldPath"] = strings.TrimSpace(fieldPath)
	}

	// Best-effort pod spec extraction for workload objects.
	spec := toMap(obj["spec"])
	if spec == nil {
		return out
	}
	switch kind {
	case "Pod":
		// spec already points at pod spec
	case "CronJob":
		spec = toMap(toMap(toMap(spec["jobTemplate"])["spec"])["template"])
		spec = toMap(spec["spec"])
	default:
		// Deployments/StatefulSets/DaemonSets/ReplicaSets/Jobs etc.
		if tpl := toMap(spec["template"]); tpl != nil {
			spec = toMap(tpl["spec"])
		}
	}
	if spec == nil {
		return out
	}

	if v := spec["automountServiceAccountToken"]; v != nil {
		out["automountServiceAccountToken"] = v
	}
	if v := strings.TrimSpace(toString(spec["serviceAccountName"])); v != "" {
		out["serviceAccountName"] = v
	}
	if v := spec["hostNetwork"]; v != nil {
		out["hostNetwork"] = v
	}
	if v := spec["hostPID"]; v != nil {
		out["hostPID"] = v
	}

	containers := toSlice(spec["containers"])
	if len(containers) == 0 {
		return out
	}
	var cOut []map[string]any
	for _, c := range containers {
		cm := toMap(c)
		if cm == nil {
			continue
		}
		entry := map[string]any{}
		if v := strings.TrimSpace(toString(cm["name"])); v != "" {
			entry["name"] = v
		}
		if v := strings.TrimSpace(toString(cm["image"])); v != "" {
			entry["image"] = v
		}
		sec := toMap(cm["securityContext"])
		if sec != nil {
			secOut := map[string]any{}
			for _, k := range []string{"privileged", "runAsNonRoot", "allowPrivilegeEscalation", "readOnlyRootFilesystem"} {
				if v := sec[k]; v != nil {
					secOut[k] = v
				}
			}
			if caps := toMap(sec["capabilities"]); caps != nil {
				if drop := caps["drop"]; drop != nil {
					secOut["capabilities.drop"] = drop
				}
			}
			if len(secOut) > 0 {
				entry["securityContext"] = secOut
			}
		}
		res := toMap(cm["resources"])
		if res != nil {
			rOut := map[string]any{}
			if req := toMap(res["requests"]); req != nil {
				if v := req["memory"]; v != nil {
					rOut["requests.memory"] = v
				}
			}
			if lim := toMap(res["limits"]); lim != nil {
				if v := lim["memory"]; v != nil {
					rOut["limits.memory"] = v
				}
			}
			if len(rOut) > 0 {
				entry["resources"] = rOut
			}
		}
		if len(entry) > 0 {
			cOut = append(cOut, entry)
		}
	}
	if len(cOut) > 0 {
		out["containers"] = cOut
	}
	return out
}
