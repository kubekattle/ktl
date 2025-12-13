// diff.go compares deployment events across ReplicaSets for 'ktl logs diff-deployments'.
package deploydiff

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/fatih/color"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// Options control which deployments to compare.
type Options struct {
	Namespace   string
	Deployments []string
}

// Runner streams event diffs for deployments.
type Runner struct {
	client kubernetes.Interface
	opts   Options
}

// New creates a Runner instance.
func New(client kubernetes.Interface, opts Options) *Runner {
	return &Runner{client: client, opts: opts}
}

// Run begins streaming and diffing deployment events.
func (r *Runner) Run(ctx context.Context) error {
	if r.opts.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if len(r.opts.Deployments) == 0 {
		return fmt.Errorf("at least one deployment must be specified")
	}

	podMap, err := r.classifyPods(ctx)
	if err != nil {
		return err
	}
	printHeader(podMap)

	return r.streamEvents(ctx, podMap)
}

type podClassification struct {
	NewPods map[string]struct{}
	OldPods map[string]struct{}
}

func (r *Runner) classifyPods(ctx context.Context) (map[string]podClassification, error) {
	result := make(map[string]podClassification)
	for _, deployName := range r.opts.Deployments {
		deploy, err := r.client.AppsV1().Deployments(r.opts.Namespace).Get(ctx, deployName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get deployment %s: %w", deployName, err)
		}
		replicaSets, err := r.client.AppsV1().ReplicaSets(r.opts.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(deploy.Spec.Selector),
		})
		if err != nil {
			return nil, fmt.Errorf("list replicasets for %s: %w", deployName, err)
		}
		newRS := newestReplicaSet(replicaSets.Items)
		pods, err := r.client.CoreV1().Pods(r.opts.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(deploy.Spec.Selector),
		})
		if err != nil {
			return nil, fmt.Errorf("list pods for %s: %w", deployName, err)
		}
		classification := podClassification{
			NewPods: map[string]struct{}{},
			OldPods: map[string]struct{}{},
		}
		for _, pod := range pods.Items {
			rsName := owningReplicaSet(pod.OwnerReferences)
			if rsName == "" {
				classification.OldPods[pod.Name] = struct{}{}
				continue
			}
			if newRS != nil && rsName == newRS.Name {
				classification.NewPods[pod.Name] = struct{}{}
			} else {
				classification.OldPods[pod.Name] = struct{}{}
			}
		}
		result[deployName] = classification
	}
	return result, nil
}

func newestReplicaSet(sets []appsv1.ReplicaSet) *appsv1.ReplicaSet {
	if len(sets) == 0 {
		return nil
	}
	sort.Slice(sets, func(i, j int) bool {
		revI := revision(sets[i])
		revJ := revision(sets[j])
		if revI == revJ {
			return sets[i].CreationTimestamp.After(sets[j].CreationTimestamp.Time)
		}
		return revI > revJ
	})
	return &sets[0]
}

func revision(rs appsv1.ReplicaSet) int64 {
	if val, ok := rs.Annotations["deployment.kubernetes.io/revision"]; ok {
		if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func owningReplicaSet(owners []metav1.OwnerReference) string {
	for _, owner := range owners {
		if owner.Kind == "ReplicaSet" && owner.Controller != nil && *owner.Controller {
			return owner.Name
		}
	}
	return ""
}

func (r *Runner) streamEvents(ctx context.Context, podMap map[string]podClassification) error {
	watcher, err := r.client.CoreV1().Events(r.opts.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.AndSelectors(
			fields.OneTermEqualSelector("involvedObject.namespace", r.opts.Namespace),
		).String(),
	})
	if err != nil {
		return fmt.Errorf("watch events: %w", err)
	}
	defer watcher.Stop()

	newCounts := make(map[string]int)
	oldCounts := make(map[string]int)

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("event stream closed")
			}
			k8sEvent, _ := evt.Object.(*corev1.Event)
			if k8sEvent == nil {
				continue
			}
			r.processEvent(k8sEvent, podMap, newCounts, oldCounts)
		}
	}
}

func (r *Runner) processEvent(event *corev1.Event, podMap map[string]podClassification, newCounts, oldCounts map[string]int) {
	podName := event.InvolvedObject.Name
	if podName == "" {
		return
	}
	if event.InvolvedObject.Kind != "Pod" {
		return
	}
	var deployment string
	var classification string
	for deploy, classes := range podMap {
		if _, ok := classes.NewPods[podName]; ok {
			deployment = deploy
			classification = "new"
			break
		}
		if _, ok := classes.OldPods[podName]; ok {
			deployment = deploy
			classification = "old"
			break
		}
	}
	if deployment == "" {
		return
	}

	key := fmt.Sprintf("%s|%s", deployment, event.Reason)
	msg := fmt.Sprintf("[%s] %s/%s %s: %s", classificationUpper(classification), event.InvolvedObject.Namespace, podName, event.Reason, strings.TrimSpace(event.Message))

	if classification == "new" {
		newCounts[key]++
		fmt.Println(colorize(msg, true))
	} else {
		oldCounts[key]++
		fmt.Println(colorize(msg, false))
	}

	diff := newCounts[key] - oldCounts[key]
	if diff >= 3 {
		fmt.Println(colorize(fmt.Sprintf(">>> %s reporting %d more %s events than old pods", deployment, diff, event.Reason), true))
	}
}

func classificationUpper(classification string) string {
	if classification == "new" {
		return "NEW"
	}
	return "OLD"
}

func colorize(text string, isNew bool) string {
	if color.NoColor {
		return text
	}
	if isNew {
		return color.New(color.FgHiGreen).Sprint(text)
	}
	return color.New(color.FgYellow).Sprint(text)
}

func printHeader(podMap map[string]podClassification) {
	for deploy, classification := range podMap {
		fmt.Printf("Deployment %s\n", deploy)
		fmt.Printf("  New pods: %s\n", strings.Join(mapKeys(classification.NewPods), ", "))
		fmt.Printf("  Old pods: %s\n", strings.Join(mapKeys(classification.OldPods), ", "))
	}
	fmt.Println("Streaming events (NEW=green, OLD=yellow)...")
}

func mapKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return []string{"-"}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
