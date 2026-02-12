package remote

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/klog/v2"

	"github.com/kubekattle/ktl/internal/config"
	"github.com/kubekattle/ktl/internal/logging"
	"github.com/kubekattle/ktl/internal/tailer"
)

const (
	buildkitImage = "moby/buildkit:v0.12.5" // Pinning version for stability
	buildkitPort  = 1234
)

// BuilderManager handles the lifecycle of ephemeral remote builders.
type BuilderManager struct {
	kubeClient kubernetes.Interface
	restConfig *rest.Config
	namespace  string
	stdout     io.Writer
}

// NewBuilderManager creates a new BuilderManager.
// It assumes the kubernetes client and namespace are already configured.
func NewBuilderManager(kubeClient kubernetes.Interface, restConfig *rest.Config, namespace string, stdout io.Writer) *BuilderManager {
	if stdout == nil {
		stdout = os.Stdout
	}
	return &BuilderManager{
		kubeClient: kubeClient,
		restConfig: restConfig,
		namespace:  namespace,
		stdout:     stdout,
	}
}

// ProvisionEphemeralBuilder creates a pod, waits for it, port-forwards, and returns the address.
func (m *BuilderManager) ProvisionEphemeralBuilder(ctx context.Context) (string, func(), error) {
	podName := "ktl-builder"
	klog.Infof("Provisioning/Reusing remote builder pod: %s/%s", m.namespace, podName)

	// Ensure PVC for caching
	pvcName := "ktl-builder-cache"
	useCache := false
	if err := m.ensurePVC(ctx, pvcName); err != nil {
		klog.Warningf("Failed to ensure cache PVC, proceeding without cache: %v", err)
	} else {
		useCache = true
	}

	// 1. Check for existing Pod
	existingPod, err := m.kubeClient.CoreV1().Pods(m.namespace).Get(ctx, podName, metav1.GetOptions{})
	var pod *corev1.Pod
	reused := false

	if err == nil {
		if existingPod.Status.Phase == corev1.PodRunning {
			klog.Infof("Reusing existing builder pod %s", podName)
			pod = existingPod
			reused = true
		} else {
			// Cleanup dead/pending pod to be safe
			klog.Infof("Deleting non-running builder pod %s (phase: %s)", podName, existingPod.Status.Phase)
			_ = m.kubeClient.CoreV1().Pods(m.namespace).Delete(ctx, podName, metav1.DeleteOptions{})
			// Wait for deletion (simple poll)
			_ = wait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Second, true, func(ctx context.Context) (bool, error) {
				_, err := m.kubeClient.CoreV1().Pods(m.namespace).Get(ctx, podName, metav1.GetOptions{})
				return apierrors.IsNotFound(err), nil
			})
		}
	}

	if !reused {
		// Create Pod
		hostPathDir := corev1.HostPathDirectory
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: m.namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":       "ktl-builder",
					"app.kubernetes.io/managed-by": "ktl",
				},
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:    "clean-lock",
						Image:   "busybox",
						Command: []string{"sh", "-c", "rm -f /var/lib/buildkit/buildkitd.lock && mkdir -p /var/lib/buildkit && chmod 777 /var/lib/buildkit"},
					},
					{
						Name:  "binfmt",
						Image: "tonistiigi/binfmt:qemu-v7.0.0-28",
						Args:  []string{"--install", "all"},
						SecurityContext: &corev1.SecurityContext{
							Privileged: boolPtr(true),
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "buildkitd",
						Image: buildkitImage,
						Args: []string{
							"--addr", "tcp://0.0.0.0:1234",
							"--oci-worker=true",
							"--containerd-worker=false",
							"--debug",
						},
						SecurityContext: &corev1.SecurityContext{
							Privileged: boolPtr(true),
						},
						Ports: []corev1.ContainerPort{
							{ContainerPort: buildkitPort},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "cgroup",
								MountPath: "/sys/fs/cgroup",
							},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{"buildctl", "debug", "workers"},
								},
							},
							InitialDelaySeconds: 10,
							PeriodSeconds:       10,
							TimeoutSeconds:      5,
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyAlways,
				Volumes: []corev1.Volume{
					{
						Name: "cgroup",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/sys/fs/cgroup",
								Type: &hostPathDir,
							},
						},
					},
				},
			},
		}

		if useCache {
			pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
				Name: "cache",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName,
					},
				},
			})
			pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
				Name:      "cache",
				MountPath: "/var/lib/buildkit",
			})
			pod.Spec.InitContainers[0].VolumeMounts = append(pod.Spec.InitContainers[0].VolumeMounts, corev1.VolumeMount{
				Name:      "cache",
				MountPath: "/var/lib/buildkit",
			})
		}

		_, err = m.kubeClient.CoreV1().Pods(m.namespace).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			return "", nil, errors.Wrap(err, "failed to create builder pod")
		}
	}

	cleanup := func() {
		// Smart cleanup: Don't delete if we are reusing!
		if !reused {
			// Keep it running for future use!
			// fmt.Fprintf(m.stdout, "Keeping builder pod running for future builds\n")
		}
	}

	// 2. Wait for Pod Ready
	fmt.Fprintf(m.stdout, "Waiting for builder pod to be ready...\n")

	// Start streaming logs in background
	ctxStream, cancelStream := context.WithCancel(ctx)
	go func() {
		opts := &config.Options{
			Namespaces: []string{m.namespace},
			PodQuery:   podName,
			Follow:     true,
			ColorMode:  "always",
		}
		logger, _ := logging.New("info")
		t, err := tailer.New(m.kubeClient, opts, logger)
		if err == nil {
			// Create a pipe to prefix logs or just write to stdout
			// For simplicity, we use the manager's stdout
			// Note: Tailer writes to os.Stdout by default, we need to override
			_ = t.Run(ctxStream)
		}
	}()
	defer cancelStream()

	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		p, err := m.kubeClient.CoreV1().Pods(m.namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range p.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		// Diagnose failure
		p, _ := m.kubeClient.CoreV1().Pods(m.namespace).Get(ctx, podName, metav1.GetOptions{})
		if p != nil {
			fmt.Fprintf(m.stdout, "\nPod Status: %s\n", p.Status.Phase)
			for _, cond := range p.Status.Conditions {
				fmt.Fprintf(m.stdout, "Condition %s: %s (Reason: %s, Message: %s)\n", cond.Type, cond.Status, cond.Reason, cond.Message)
			}
			// Fetch events
			events, _ := m.kubeClient.CoreV1().Events(m.namespace).List(ctx, metav1.ListOptions{
				FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
			})
			if events != nil {
				fmt.Fprintf(m.stdout, "\nEvents:\n")
				for _, e := range events.Items {
					fmt.Fprintf(m.stdout, "- [%s] %s: %s\n", e.Type, e.Reason, e.Message)
				}
			}
		}
		return "", nil, errors.Wrap(err, "builder pod failed to become ready")
	}

	// 3. Port Forward
	localPort, stopChan, err := m.portForward(ctx, podName, buildkitPort)
	if err != nil {
		cleanup()
		return "", nil, errors.Wrap(err, "failed to port-forward")
	}

	// Enhance cleanup to stop port-forwarding
	fullCleanup := func() {
		close(stopChan)
		cleanup()
	}

	addr := fmt.Sprintf("tcp://127.0.0.1:%d", localPort)
	klog.Infof("Remote builder ready at %s", addr)
	return addr, fullCleanup, nil
}

func (m *BuilderManager) portForward(ctx context.Context, podName string, remotePort int) (int, chan struct{}, error) {
	// Find a free local port
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, nil, err
	}
	localPort := l.Addr().(*net.TCPAddr).Port
	l.Close()

	// Create request URL
	req := m.kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(m.namespace).
		Name(podName).
		SubResource("portforward")

	// Create round tripper and upgrader
	transport, upgrader, err := spdy.RoundTripperFor(m.restConfig)
	if err != nil {
		return 0, nil, err
	}

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	// We need to capture stdout/stderr to debug if it fails
	// But for now, discard
	pf, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, remotePort)}, stopChan, readyChan, os.Stdout, os.Stderr)
	if err != nil {
		return 0, nil, err
	}

	go func() {
		if err := pf.ForwardPorts(); err != nil {
			klog.Errorf("Port forwarding failed: %v", err)
		}
	}()

	select {
	case <-readyChan:
		return localPort, stopChan, nil
	case <-time.After(10 * time.Second):
		close(stopChan)
		return 0, nil, fmt.Errorf("timeout waiting for port-forward")
	}
}

// IsLocalDockerAvailable checks if docker is running locally
func IsLocalDockerAvailable() bool {
	cmd := exec.Command("docker", "version")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func boolPtr(b bool) *bool {
	return &b
}

func (m *BuilderManager) ensurePVC(ctx context.Context, name string) error {
	_, err := m.kubeClient.CoreV1().PersistentVolumeClaims(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "ktl-builder",
				"app.kubernetes.io/managed-by": "ktl",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	_, err = m.kubeClient.CoreV1().PersistentVolumeClaims(m.namespace).Create(ctx, pvc, metav1.CreateOptions{})
	return err
}
