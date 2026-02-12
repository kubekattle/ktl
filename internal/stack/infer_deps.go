package stack

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/kubekattle/ktl/internal/deploy"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type InferDepsOptions struct {
	IncludeConfigRefs bool
	Secrets           *deploy.SecretOptions
}

// InferDependencies renders each release client-side and infers additional Needs edges
// between releases in the same cluster. The inference is deterministic for identical
// inputs (chart+values+set+helm version) because it runs with UseCluster=false.
func InferDependencies(ctx context.Context, p *Plan, defaultKubeconfig string, defaultKubeContext string, opts InferDepsOptions) error {
	if p == nil {
		return fmt.Errorf("plan is nil")
	}

	for clusterName, nodes := range p.ByCluster {
		if err := inferClusterDependencies(ctx, clusterName, nodes, defaultKubeconfig, defaultKubeContext, opts); err != nil {
			return err
		}
	}
	return nil
}

type releaseFacts struct {
	role string

	namespacesProvided map[string][]InferredReason // namespace -> reasons
	crdsProvided       map[string][]InferredReason // group/kind -> reasons
	saProvided         map[string][]InferredReason // ns/name -> reasons
	rbacForSA          map[string][]InferredReason // ns/name -> reasons (rolebindings targeting SA)
	pvcProvided        map[string][]InferredReason // ns/name -> reasons
	cfgProvided        map[string][]InferredReason // ns/name -> reasons
	secProvided        map[string][]InferredReason // ns/name -> reasons

	objects []*unstructured.Unstructured
}

func inferClusterDependencies(ctx context.Context, clusterName string, nodes []*ResolvedRelease, defaultKubeconfig string, defaultKubeContext string, opts InferDepsOptions) error {
	if len(nodes) == 0 {
		return nil
	}
	byName := map[string]*ResolvedRelease{}
	for _, n := range nodes {
		byName[n.Name] = n
	}

	// Render each release once and collect facts.
	factsByID := map[string]*releaseFacts{}
	for _, n := range nodes {
		objs, role, facts, err := renderAndExtractFacts(ctx, n, defaultKubeconfig, defaultKubeContext, opts)
		if err != nil {
			return fmt.Errorf("infer deps (%s): %w", n.ID, err)
		}
		facts.objects = objs
		facts.role = role
		factsByID[n.ID] = facts
		// Keep the strongest (earliest) role for deterministic scheduling.
		if n.InferredRole == "" || releaseRoleOrder(role) < releaseRoleOrder(n.InferredRole) {
			n.InferredRole = role
		}
	}

	// Provider indices (cluster-scoped within this stack cluster).
	nsProviders := map[string][]provider{}
	crdProviders := map[string][]provider{}
	saProviders := map[string][]provider{}
	rbacForSAProviders := map[string][]provider{}
	pvcProviders := map[string][]provider{}
	cfgProviders := map[string][]provider{}
	secProviders := map[string][]provider{}
	for _, n := range nodes {
		f := factsByID[n.ID]
		for ns, reasons := range f.namespacesProvided {
			nsProviders[ns] = append(nsProviders[ns], provider{node: n, reasons: reasons})
		}
		for gk, reasons := range f.crdsProvided {
			crdProviders[gk] = append(crdProviders[gk], provider{node: n, reasons: reasons})
		}
		for key, reasons := range f.saProvided {
			saProviders[key] = append(saProviders[key], provider{node: n, reasons: reasons})
		}
		for key, reasons := range f.rbacForSA {
			rbacForSAProviders[key] = append(rbacForSAProviders[key], provider{node: n, reasons: reasons})
		}
		for key, reasons := range f.pvcProvided {
			pvcProviders[key] = append(pvcProviders[key], provider{node: n, reasons: reasons})
		}
		for key, reasons := range f.cfgProvided {
			cfgProviders[key] = append(cfgProviders[key], provider{node: n, reasons: reasons})
		}
		for key, reasons := range f.secProvided {
			secProviders[key] = append(secProviders[key], provider{node: n, reasons: reasons})
		}
	}
	sortProviders := func(m map[string][]provider) {
		for k := range m {
			sort.Slice(m[k], func(i, j int) bool { return m[k][i].node.Name < m[k][j].node.Name })
		}
	}
	sortProviders(nsProviders)
	sortProviders(crdProviders)
	sortProviders(saProviders)
	sortProviders(rbacForSAProviders)
	sortProviders(pvcProviders)
	sortProviders(cfgProviders)
	sortProviders(secProviders)

	for _, n := range nodes {
		f := factsByID[n.ID]
		inferred := map[string][]InferredReason{} // depName -> reasons
		add := func(depName string, reason InferredReason) {
			depName = strings.TrimSpace(depName)
			if depName == "" || depName == n.Name {
				return
			}
			if _, ok := byName[depName]; !ok {
				// If a chart references a release that doesn't exist, keep it out of the plan.
				return
			}
			inferred[depName] = append(inferred[depName], reason)
		}

		for _, obj := range f.objects {
			if obj == nil {
				continue
			}
			objNS := effectiveObjectNamespace(obj, n.Namespace)
			ann := obj.GetAnnotations()
			if ann != nil {
				if raw := strings.TrimSpace(ann["ktl.io/depends-on"]); raw != "" {
					for _, dep := range splitCSVList(raw) {
						add(dep, InferredReason{
							Type:     "annotation:depends-on",
							Evidence: fmt.Sprintf("%s %s/%s", obj.GetKind(), objNS, obj.GetName()),
						})
					}
				}
			}

			kind := strings.TrimSpace(obj.GetKind())
			apiVersion := strings.TrimSpace(obj.GetAPIVersion())
			group := strings.TrimSpace(groupFromAPIVersion(apiVersion))
			ns := strings.TrimSpace(objNS)
			name := strings.TrimSpace(obj.GetName())
			if kind == "" || name == "" {
				continue
			}

			// Namespace provider dependencies: only when another release explicitly provides the namespace.
			if ns != "" && kind != "Namespace" {
				if providers := nsProviders[ns]; len(providers) > 0 {
					for _, pr := range providers {
						if pr.node.ID == n.ID {
							continue
						}
						add(pr.node.Name, InferredReason{Type: "namespace", Evidence: fmt.Sprintf("ns/%s", ns)})
					}
				}
			}

			// CRD -> CR dependency: if this object's group/kind matches a provided CRD.
			if group != "" && kind != "CustomResourceDefinition" && kind != "APIService" && kind != "Namespace" {
				gk := strings.ToLower(group) + "/" + strings.ToLower(kind)
				if providers := crdProviders[gk]; len(providers) > 0 {
					for _, pr := range providers {
						if pr.node.ID == n.ID {
							continue
						}
						add(pr.node.Name, InferredReason{Type: "crd", Evidence: fmt.Sprintf("%s.%s", kind, group)})
					}
				}
			}

			// Workload -> ServiceAccount dependency.
			if sa := workloadServiceAccountName(obj); sa != "" && ns != "" {
				key := strings.ToLower(ns) + "/" + strings.ToLower(sa)
				// Prefer RBAC providers that explicitly bind roles to this SA.
				if providers := rbacForSAProviders[key]; len(providers) > 0 {
					for _, pr := range providers {
						if pr.node.ID == n.ID {
							continue
						}
						add(pr.node.Name, InferredReason{Type: "rbac", Evidence: fmt.Sprintf("sa/%s in ns/%s", sa, ns)})
					}
				} else if providers := saProviders[key]; len(providers) > 0 {
					for _, pr := range providers {
						if pr.node.ID == n.ID {
							continue
						}
						add(pr.node.Name, InferredReason{Type: "serviceAccount", Evidence: fmt.Sprintf("sa/%s in ns/%s", sa, ns)})
					}
				}
			}

			// Workload -> PVC dependency.
			if ns != "" {
				for _, pvc := range workloadPVCRefs(obj) {
					key := strings.ToLower(ns) + "/" + strings.ToLower(pvc)
					if providers := pvcProviders[key]; len(providers) > 0 {
						for _, pr := range providers {
							if pr.node.ID == n.ID {
								continue
							}
							add(pr.node.Name, InferredReason{Type: "pvc", Evidence: fmt.Sprintf("pvc/%s in ns/%s", pvc, ns)})
						}
					}
				}
			}

			if opts.IncludeConfigRefs && ns != "" {
				for _, cm := range workloadConfigMapRefs(obj) {
					key := strings.ToLower(ns) + "/" + strings.ToLower(cm)
					if providers := cfgProviders[key]; len(providers) > 0 {
						for _, pr := range providers {
							if pr.node.ID == n.ID {
								continue
							}
							add(pr.node.Name, InferredReason{Type: "configMap", Evidence: fmt.Sprintf("cm/%s in ns/%s", cm, ns)})
						}
					}
				}
				for _, sec := range workloadSecretRefs(obj) {
					key := strings.ToLower(ns) + "/" + strings.ToLower(sec)
					if providers := secProviders[key]; len(providers) > 0 {
						for _, pr := range providers {
							if pr.node.ID == n.ID {
								continue
							}
							add(pr.node.Name, InferredReason{Type: "secret", Evidence: fmt.Sprintf("secret/%s in ns/%s", sec, ns)})
						}
					}
				}
			}
		}

		// Materialize inferred edges into stable InferredNeeds + merged Needs.
		var inferredList []InferredNeed
		for depName, reasons := range inferred {
			sort.Slice(reasons, func(i, j int) bool {
				if reasons[i].Type != reasons[j].Type {
					return reasons[i].Type < reasons[j].Type
				}
				return reasons[i].Evidence < reasons[j].Evidence
			})
			inferredList = append(inferredList, InferredNeed{Name: depName, Reasons: reasons})
		}
		sort.Slice(inferredList, func(i, j int) bool { return inferredList[i].Name < inferredList[j].Name })
		n.InferredNeeds = inferredList

		if len(inferredList) > 0 {
			seen := map[string]struct{}{}
			for _, dep := range n.Needs {
				seen[dep] = struct{}{}
			}
			for _, dep := range inferredList {
				if _, ok := seen[dep.Name]; ok {
					continue
				}
				n.Needs = append(n.Needs, dep.Name)
			}
			sort.Strings(n.Needs)
		}
	}

	return nil
}

type provider struct {
	node    *ResolvedRelease
	reasons []InferredReason
}

func renderAndExtractFacts(ctx context.Context, node *ResolvedRelease, defaultKubeconfig string, defaultKubeContext string, opts InferDepsOptions) ([]*unstructured.Unstructured, string, *releaseFacts, error) {
	kubeconfigPath := strings.TrimSpace(expandTilde(node.Cluster.Kubeconfig))
	if kubeconfigPath == "" {
		kubeconfigPath = strings.TrimSpace(defaultKubeconfig)
	}
	kubeCtx := strings.TrimSpace(node.Cluster.Context)
	if kubeCtx == "" {
		kubeCtx = strings.TrimSpace(defaultKubeContext)
	}
	// For deterministic client-only rendering, we still need a Helm settings object
	// (for chart resolution/caches). If kubeconfig is empty, Helm will fall back to
	// its defaults; keep this best-effort to avoid breaking stack plan in offline modes.
	settings := cli.New()
	if kubeconfigPath != "" {
		settings.KubeConfig = kubeconfigPath
	}
	if kubeCtx != "" {
		settings.KubeContext = kubeCtx
	}
	// Init helm action config once per render; client-only still expects Configuration to be initialized.
	actionCfg := new(action.Configuration)
	if err := actionCfg.Init(settings.RESTClientGetter(), "default", os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
		return nil, "", nil, fmt.Errorf("init helm action config: %w", err)
	}

	rendered, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
		Chart:       node.Chart,
		Version:     node.ChartVersion,
		ReleaseName: node.Name,
		Namespace:   node.Namespace,
		ValuesFiles: node.Values,
		SetValues:   flattenSet(node.Set),
		IncludeCRDs: true,
		UseCluster:  false,
		Secrets:     opts.Secrets,
	})
	if err != nil {
		return nil, "", nil, err
	}
	manifest := ""
	if rendered != nil {
		manifest = rendered.Manifest
	}

	objs, err := parseManifestObjects(manifest)
	if err != nil {
		return nil, "", nil, err
	}

	f := &releaseFacts{
		namespacesProvided: map[string][]InferredReason{},
		crdsProvided:       map[string][]InferredReason{},
		saProvided:         map[string][]InferredReason{},
		rbacForSA:          map[string][]InferredReason{},
		pvcProvided:        map[string][]InferredReason{},
		cfgProvided:        map[string][]InferredReason{},
		secProvided:        map[string][]InferredReason{},
	}
	role := ""
	setRole := func(r string) {
		if role == "" || releaseRoleOrder(r) < releaseRoleOrder(role) {
			role = r
		}
	}

	var primaryKind string
	setPrimaryKind := func(kind string) {
		if kind == "" {
			return
		}
		if primaryKind == "" {
			primaryKind = kind
			return
		}
		// Prefer "heavier" kinds for per-kind caps (heuristic).
		priority := func(k string) int {
			switch k {
			case "StatefulSet":
				return 10
			case "Deployment":
				return 20
			case "DaemonSet":
				return 30
			case "Job":
				return 40
			case "CronJob":
				return 50
			case "Pod":
				return 60
			default:
				return 90
			}
		}
		if priority(kind) < priority(primaryKind) {
			primaryKind = kind
		}
	}

	for _, obj := range objs {
		if obj == nil {
			continue
		}
		ann := obj.GetAnnotations()
		if ann != nil {
			// Release-level overrides via any rendered object annotation.
			// Config file values still win because they are already applied to the node before inference.
			if node.Wave == 0 {
				if raw := strings.TrimSpace(ann["ktl.io/wave"]); raw != "" {
					if v, err := parseInt(raw); err == nil {
						node.Wave = v
					}
				}
			}
			if !node.Critical {
				if raw := strings.TrimSpace(ann["ktl.io/critical"]); raw == "1" || strings.EqualFold(raw, "true") {
					node.Critical = true
				}
			}
			if strings.TrimSpace(node.Parallelism) == "" {
				if raw := strings.TrimSpace(ann["ktl.io/parallelismGroup"]); raw != "" {
					node.Parallelism = raw
				}
			}
		}
		kind := strings.TrimSpace(obj.GetKind())
		ns := strings.TrimSpace(effectiveObjectNamespace(obj, node.Namespace))
		name := strings.TrimSpace(obj.GetName())
		switch kind {
		case "Namespace":
			if name != "" {
				f.namespacesProvided[name] = append(f.namespacesProvided[name], InferredReason{Type: "namespace", Evidence: fmt.Sprintf("Namespace/%s", name)})
				setRole("namespace")
			}
		case "CustomResourceDefinition":
			if group, crdKind := crdGroupKind(obj); group != "" && crdKind != "" {
				gk := strings.ToLower(group) + "/" + strings.ToLower(crdKind)
				f.crdsProvided[gk] = append(f.crdsProvided[gk], InferredReason{Type: "crd", Evidence: fmt.Sprintf("%s.%s", crdKind, group)})
				setRole("crd")
			}
		case "ServiceAccount":
			if ns != "" && name != "" {
				key := strings.ToLower(ns) + "/" + strings.ToLower(name)
				f.saProvided[key] = append(f.saProvided[key], InferredReason{Type: "serviceAccount", Evidence: fmt.Sprintf("sa/%s in ns/%s", name, ns)})
				setRole("rbac")
			}
		case "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding":
			setRole("rbac")
			if kind == "RoleBinding" || kind == "ClusterRoleBinding" {
				for _, saKey := range rbacBoundServiceAccounts(obj, ns) {
					f.rbacForSA[saKey] = append(f.rbacForSA[saKey], InferredReason{Type: "rbac", Evidence: fmt.Sprintf("%s/%s", kind, name)})
				}
			}
		case "ValidatingWebhookConfiguration", "MutatingWebhookConfiguration", "ValidatingAdmissionPolicy":
			setRole("webhook")
		case "PersistentVolumeClaim":
			if ns != "" && name != "" {
				key := strings.ToLower(ns) + "/" + strings.ToLower(name)
				f.pvcProvided[key] = append(f.pvcProvided[key], InferredReason{Type: "pvc", Evidence: fmt.Sprintf("pvc/%s in ns/%s", name, ns)})
			}
		case "ConfigMap":
			if ns != "" && name != "" {
				key := strings.ToLower(ns) + "/" + strings.ToLower(name)
				f.cfgProvided[key] = append(f.cfgProvided[key], InferredReason{Type: "configMap", Evidence: fmt.Sprintf("cm/%s in ns/%s", name, ns)})
			}
		case "Secret":
			if ns != "" && name != "" {
				key := strings.ToLower(ns) + "/" + strings.ToLower(name)
				f.secProvided[key] = append(f.secProvided[key], InferredReason{Type: "secret", Evidence: fmt.Sprintf("secret/%s in ns/%s", name, ns)})
			}
		default:
			setRole("workload")
			setPrimaryKind(kind)
		}
	}

	if role == "" {
		role = "workload"
	}
	node.InferredPrimaryKind = primaryKind
	return objs, role, f, nil
}

func parseManifestObjects(manifest string) ([]*unstructured.Unstructured, error) {
	manifest = strings.TrimSpace(manifest)
	if manifest == "" {
		return nil, nil
	}

	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []*unstructured.Unstructured
	for {
		var raw map[string]any
		err := dec.Decode(&raw)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode manifest: %w", err)
		}
		if len(raw) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{Object: raw}
		if strings.TrimSpace(obj.GetKind()) == "" || strings.TrimSpace(obj.GetName()) == "" {
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

func effectiveObjectNamespace(obj *unstructured.Unstructured, defaultNamespace string) string {
	if obj == nil {
		return strings.TrimSpace(defaultNamespace)
	}
	ns := strings.TrimSpace(obj.GetNamespace())
	if ns != "" {
		return ns
	}
	kind := strings.TrimSpace(obj.GetKind())
	if isClusterScopedKind(kind) {
		return ""
	}
	ns = strings.TrimSpace(defaultNamespace)
	if ns == "" {
		return "default"
	}
	return ns
}

func isClusterScopedKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "Namespace",
		"Node",
		"PersistentVolume",
		"CustomResourceDefinition",
		"APIService",
		"ClusterRole",
		"ClusterRoleBinding",
		"ValidatingWebhookConfiguration",
		"MutatingWebhookConfiguration",
		"ValidatingAdmissionPolicy",
		"StorageClass",
		"PriorityClass":
		return true
	default:
		return false
	}
}

func groupFromAPIVersion(apiVersion string) string {
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return ""
	}
	parts := strings.Split(apiVersion, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func crdGroupKind(obj *unstructured.Unstructured) (string, string) {
	if obj == nil {
		return "", ""
	}
	group, _, _ := unstructured.NestedString(obj.Object, "spec", "group")
	kind, _, _ := unstructured.NestedString(obj.Object, "spec", "names", "kind")
	return strings.TrimSpace(group), strings.TrimSpace(kind)
}

func workloadServiceAccountName(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	kind := strings.TrimSpace(obj.GetKind())
	var sa string
	switch kind {
	case "Pod":
		sa, _, _ = unstructured.NestedString(obj.Object, "spec", "serviceAccountName")
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet":
		sa, _, _ = unstructured.NestedString(obj.Object, "spec", "template", "spec", "serviceAccountName")
	case "Job":
		sa, _, _ = unstructured.NestedString(obj.Object, "spec", "template", "spec", "serviceAccountName")
	case "CronJob":
		sa, _, _ = unstructured.NestedString(obj.Object, "spec", "jobTemplate", "spec", "template", "spec", "serviceAccountName")
	}
	sa = strings.TrimSpace(sa)
	if sa == "" || strings.EqualFold(sa, "default") {
		return ""
	}
	return sa
}

func workloadPVCRefs(obj *unstructured.Unstructured) []string {
	vols := workloadVolumes(obj)
	var out []string
	for _, v := range vols {
		if claim, ok := v["persistentVolumeClaim"].(map[string]any); ok {
			if name, _ := claim["claimName"].(string); strings.TrimSpace(name) != "" {
				out = append(out, strings.TrimSpace(name))
			}
		}
	}
	sort.Strings(out)
	out = dedupeStringsInfer(out)
	return out
}

func workloadConfigMapRefs(obj *unstructured.Unstructured) []string {
	vols := workloadVolumes(obj)
	var out []string
	for _, v := range vols {
		if cm, ok := v["configMap"].(map[string]any); ok {
			if name, _ := cm["name"].(string); strings.TrimSpace(name) != "" {
				out = append(out, strings.TrimSpace(name))
			}
		}
	}
	out = append(out, workloadEnvConfigMapRefs(obj)...)
	sort.Strings(out)
	out = dedupeStringsInfer(out)
	return out
}

func workloadSecretRefs(obj *unstructured.Unstructured) []string {
	vols := workloadVolumes(obj)
	var out []string
	for _, v := range vols {
		if sec, ok := v["secret"].(map[string]any); ok {
			if name, _ := sec["secretName"].(string); strings.TrimSpace(name) != "" {
				out = append(out, strings.TrimSpace(name))
			}
		}
	}
	out = append(out, workloadEnvSecretRefs(obj)...)
	sort.Strings(out)
	out = dedupeStringsInfer(out)
	return out
}

func workloadVolumes(obj *unstructured.Unstructured) []map[string]any {
	if obj == nil {
		return nil
	}
	kind := strings.TrimSpace(obj.GetKind())
	var vols []any
	switch kind {
	case "Pod":
		vols, _, _ = unstructured.NestedSlice(obj.Object, "spec", "volumes")
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job":
		vols, _, _ = unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
	case "CronJob":
		vols, _, _ = unstructured.NestedSlice(obj.Object, "spec", "jobTemplate", "spec", "template", "spec", "volumes")
	default:
		return nil
	}
	var out []map[string]any
	for _, v := range vols {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func workloadEnvConfigMapRefs(obj *unstructured.Unstructured) []string {
	envFrom, env := workloadEnv(obj)
	var out []string
	for _, item := range envFrom {
		if cm, ok := item["configMapRef"].(map[string]any); ok {
			if name, _ := cm["name"].(string); strings.TrimSpace(name) != "" {
				out = append(out, strings.TrimSpace(name))
			}
		}
	}
	for _, item := range env {
		if vf, ok := item["valueFrom"].(map[string]any); ok {
			if cm, ok := vf["configMapKeyRef"].(map[string]any); ok {
				if name, _ := cm["name"].(string); strings.TrimSpace(name) != "" {
					out = append(out, strings.TrimSpace(name))
				}
			}
		}
	}
	return out
}

func workloadEnvSecretRefs(obj *unstructured.Unstructured) []string {
	envFrom, env := workloadEnv(obj)
	var out []string
	for _, item := range envFrom {
		if sec, ok := item["secretRef"].(map[string]any); ok {
			if name, _ := sec["name"].(string); strings.TrimSpace(name) != "" {
				out = append(out, strings.TrimSpace(name))
			}
		}
	}
	for _, item := range env {
		if vf, ok := item["valueFrom"].(map[string]any); ok {
			if sec, ok := vf["secretKeyRef"].(map[string]any); ok {
				if name, _ := sec["name"].(string); strings.TrimSpace(name) != "" {
					out = append(out, strings.TrimSpace(name))
				}
			}
		}
	}
	return out
}

func workloadEnv(obj *unstructured.Unstructured) (envFrom []map[string]any, env []map[string]any) {
	containers := workloadContainers(obj)
	for _, c := range containers {
		if v, ok := c["envFrom"].([]any); ok {
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					envFrom = append(envFrom, m)
				}
			}
		}
		if v, ok := c["env"].([]any); ok {
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					env = append(env, m)
				}
			}
		}
	}
	return envFrom, env
}

func workloadContainers(obj *unstructured.Unstructured) []map[string]any {
	if obj == nil {
		return nil
	}
	kind := strings.TrimSpace(obj.GetKind())
	var containers []any
	switch kind {
	case "Pod":
		containers, _, _ = unstructured.NestedSlice(obj.Object, "spec", "containers")
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "Job":
		containers, _, _ = unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	case "CronJob":
		containers, _, _ = unstructured.NestedSlice(obj.Object, "spec", "jobTemplate", "spec", "template", "spec", "containers")
	default:
		return nil
	}
	var out []map[string]any
	for _, c := range containers {
		if m, ok := c.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func dedupeStringsInfer(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty")
	}
	sign := 1
	if strings.HasPrefix(raw, "-") {
		sign = -1
		raw = strings.TrimPrefix(raw, "-")
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid int %q", raw)
		}
		n = n*10 + int(r-'0')
	}
	return sign * n, nil
}

func rbacBoundServiceAccounts(obj *unstructured.Unstructured, defaultNamespace string) []string {
	if obj == nil {
		return nil
	}
	subjects, found, _ := unstructured.NestedSlice(obj.Object, "subjects")
	if !found || len(subjects) == 0 {
		return nil
	}
	var out []string
	for _, s := range subjects {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		name, _ := m["name"].(string)
		ns, _ := m["namespace"].(string)
		if !strings.EqualFold(strings.TrimSpace(kind), "ServiceAccount") {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ns = strings.TrimSpace(ns)
		if ns == "" {
			ns = strings.TrimSpace(defaultNamespace)
		}
		if ns == "" {
			ns = "default"
		}
		out = append(out, strings.ToLower(ns)+"/"+strings.ToLower(name))
	}
	sort.Strings(out)
	out = dedupeStringsInfer(out)
	return out
}

func splitCSVList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
