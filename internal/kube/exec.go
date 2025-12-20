// exec.go shells into pods/containers to run commands for db backup and other helpers.
package kube

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// Exec streams the provided command inside the target pod/container.
func (c *Client) Exec(ctx context.Context, namespace, pod, container string, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(command) == 0 {
		return fmt.Errorf("command must not be empty")
	}
	if namespace == "" {
		namespace = c.Namespace
	}
	if namespace == "" {
		return fmt.Errorf("namespace must be specified")
	}
	if pod == "" {
		return fmt.Errorf("pod must be specified")
	}

	req := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("exec")

	execOpts := &corev1.PodExecOptions{
		Command:   command,
		Container: container,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
		TTY:       false,
	}

	req.VersionedParams(execOpts, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.RESTConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create executor: %w", err)
	}

	streamOpts := remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	}

	if err := executor.StreamWithContext(ctx, streamOpts); err != nil {
		return fmt.Errorf("exec command: %w", err)
	}

	return nil
}
