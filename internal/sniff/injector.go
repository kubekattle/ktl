// injector.go injects temporary capture pods/sidecars used by the sniff package.
package sniff

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

// InjectorOptions control how sniffer containers are created.
type InjectorOptions struct {
	Image          string
	PullPolicy     corev1.PullPolicy
	Privileged     bool
	StartupTimeout time.Duration
	PollInterval   time.Duration
}

// PodRef identifies the pod and container namespace to reuse.
type PodRef struct {
	Namespace       string
	Pod             string
	TargetContainer string
}

// Injector adds ephemeral containers to pods so tcpdump can run even when the original image lacks it.
type Injector struct {
	client *kube.Client
	opts   InjectorOptions
}

// NewInjector builds an Injector with sane defaults.
func NewInjector(client *kube.Client, opts InjectorOptions) *Injector {
	if opts.StartupTimeout == 0 {
		opts.StartupTimeout = 30 * time.Second
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 500 * time.Millisecond
	}
	if opts.PullPolicy == "" {
		opts.PullPolicy = corev1.PullIfNotPresent
	}
	return &Injector{client: client, opts: opts}
}

// Ensure creates an ephemeral container inside the referenced pod and waits until it is running.
// The returned string is the container name to exec into.
func (i *Injector) Ensure(ctx context.Context, ref PodRef) (string, error) {
	if ref.Namespace == "" || ref.Pod == "" {
		return "", fmt.Errorf("namespace and pod are required")
	}
	if ref.TargetContainer == "" {
		return "", fmt.Errorf("target container is required")
	}
	name := fmt.Sprintf("ktl-sniff-%s", strings.ToLower(rand.String(5)))
	podClient := i.client.Clientset.CoreV1().Pods(ref.Namespace)

	pod, err := podClient.Get(ctx, ref.Pod, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get pod %s/%s: %w", ref.Namespace, ref.Pod, err)
	}

	stopFile := fmt.Sprintf("/tmp/ktl-sniff-%s.stop", name)
	command := fmt.Sprintf("trap 'exit 0' TERM INT; rm -f %[1]s; while [ ! -f %[1]s ]; do sleep 1; done; exit 0", stopFile)

	ephemeral := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:            name,
			Image:           i.opts.Image,
			Command:         []string{"/bin/sh", "-c", command},
			ImagePullPolicy: i.opts.PullPolicy,
			SecurityContext: buildSecurityContext(i.opts.Privileged),
		},
		TargetContainerName: ref.TargetContainer,
	}

	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ephemeral)
	if _, err := podClient.UpdateEphemeralContainers(ctx, ref.Pod, pod, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsInvalid(err) || apierrors.IsForbidden(err) {
			return "", fmt.Errorf("add sniffer container: %w", err)
		}
		return "", fmt.Errorf("update pod %s/%s: %w", ref.Namespace, ref.Pod, err)
	}

	if err := i.waitForReady(ctx, ref.Namespace, ref.Pod, name); err != nil {
		return "", err
	}
	return name, nil
}

func (i *Injector) waitForReady(ctx context.Context, namespace, podName, container string) error {
	podClient := i.client.Clientset.CoreV1().Pods(namespace)
	timeout := i.opts.StartupTimeout
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(i.opts.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pod, err := podClient.Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("check sniffer status: %w", err)
			}
			for _, status := range pod.Status.EphemeralContainerStatuses {
				if status.Name != container {
					continue
				}
				if status.State.Running != nil {
					return nil
				}
				if status.State.Terminated != nil {
					return fmt.Errorf("sniffer container %s terminated: %s", container, containerStateMessage(status.State))
				}
				if status.State.Waiting != nil {
					if status.State.Waiting.Reason == "ErrImagePull" || status.State.Waiting.Reason == "ImagePullBackOff" {
						return fmt.Errorf("sniffer image pull failed: %s", status.State.Waiting.Message)
					}
				}
			}
			if timeout > 0 && time.Now().After(deadline) {
				return fmt.Errorf("sniffer container %s did not become ready within %s", container, timeout)
			}
		}
	}
}

// Shutdown attempts to stop the helper container by signaling its init process.
func (i *Injector) Shutdown(ctx context.Context, namespace, podName, container string) error {
	if namespace == "" || podName == "" || container == "" {
		return fmt.Errorf("namespace, pod, and container are required for shutdown")
	}
	stopFile := fmt.Sprintf("/tmp/ktl-sniff-%s.stop", container)
	cmd := []string{"/bin/sh", "-c", fmt.Sprintf("touch %[1]s", stopFile)}
	if err := i.client.Exec(ctx, namespace, podName, container, cmd, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("signal sniffer %s/%s:%s: %w", namespace, podName, container, err)
	}
	if err := i.waitForTermination(ctx, namespace, podName, container); err != nil {
		// fallback to kill if stop file approach failed
		_ = i.client.Exec(ctx, namespace, podName, container, []string{"/bin/sh", "-c", "kill 1 >/dev/null 2>&1 || true"}, nil, io.Discard, io.Discard)
		return err
	}
	return nil
}

func (i *Injector) waitForTermination(ctx context.Context, namespace, podName, container string) error {
	podClient := i.client.Clientset.CoreV1().Pods(namespace)
	ticker := time.NewTicker(i.opts.PollInterval)
	defer ticker.Stop()
	timeout := i.opts.StartupTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pod, err := podClient.Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("check sniffer termination: %w", err)
			}
			for _, status := range pod.Status.EphemeralContainerStatuses {
				if status.Name != container {
					continue
				}
				if status.State.Terminated != nil {
					return nil
				}
				if status.State.Waiting != nil && status.State.Waiting.Reason == "CrashLoopBackOff" {
					return nil
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("sniffer container %s did not terminate", container)
			}
		}
	}
}

func containerStateMessage(state corev1.ContainerState) string {
	switch {
	case state.Terminated != nil:
		return fmt.Sprintf("%s: %s", state.Terminated.Reason, state.Terminated.Message)
	case state.Waiting != nil:
		return fmt.Sprintf("%s: %s", state.Waiting.Reason, state.Waiting.Message)
	default:
		return "unknown state"
	}
}

func buildSecurityContext(privileged bool) *corev1.SecurityContext {
	allowPrivEsc := true
	runAsRoot := int64(0)
	runAsNonRoot := false
	ctx := &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Add: []corev1.Capability{"NET_ADMIN", "NET_RAW", "SYS_ADMIN"},
		},
		Privileged:               boolPtr(privileged),
		AllowPrivilegeEscalation: boolPtr(allowPrivEsc),
		RunAsUser:                int64Ptr(runAsRoot),
		RunAsGroup:               int64Ptr(runAsRoot),
		RunAsNonRoot:             boolPtr(runAsNonRoot),
	}
	return ctx
}

func boolPtr(v bool) *bool {
	value := v
	return &value
}

func int64Ptr(v int64) *int64 {
	value := v
	return &value
}
