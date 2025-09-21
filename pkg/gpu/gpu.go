package gpu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// Serials represents detailed information about a single GPU.
type Serials struct {
	Chassis string   `json:"chassis" yaml:"chassis"`
	GPU     []string `json:"gpu" yaml:"gpu"`
}

// GetSerialNumbers retrieves unique GPU serial numbers from a specified pod and container.
// It executes the `nvidia-smi -q -x` command inside the container, parses the XML output,
// and extracts the serial numbers of all GPUs present. The function ensures that only
// unique serial numbers are returned, handling any duplicates that may arise.
func GetSerialNumbers(ctx context.Context, log *slog.Logger, cs *kubernetes.Clientset, cfg *rest.Config, pod *corev1.Pod, container string) ([]*Serials, error) {
	// get smi output
	stdout, err := execShell(ctx, cfg, cs, pod, container)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command in pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	b := []byte(stdout)
	_ = b

	// parse output
	d, err := parseSMIDevice(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nvidia-smi output: %w", err)
	}

	log.Debug("gpu info", "ns", pod.Namespace, "pod", pod.Name, "gpu_count", len(d.GPUs))

	// spoole through GPUs and group them by chassis serial number
	unitMap := make(map[string]map[string]bool)
	for _, gpu := range d.GPUs {
		if unitMap[gpu.PlatformInfo.ChassisSerialNumber] == nil {
			unitMap[gpu.PlatformInfo.ChassisSerialNumber] = make(map[string]bool)
		}

		if unitMap[gpu.PlatformInfo.ChassisSerialNumber][gpu.Serial] {
			continue
		}

		unitMap[gpu.PlatformInfo.ChassisSerialNumber][gpu.Serial] = true
	}

	units := make([]*Serials, 0)

	// convert map to slice
	for chassis, gpus := range unitMap {
		s := &Serials{
			Chassis: chassis,
			GPU:     make([]string, 0, len(gpus)),
		}

		for gpu := range gpus {
			s.GPU = append(s.GPU, gpu)
		}

		sort.Strings(s.GPU)
		units = append(units, s)
	}

	log.Debug("gpu serial numbers", "ns", pod.Namespace, "pod", pod.Name, "serial_numbers", len(units))

	return units, nil
}

// execShell executes a shell command in a pod container using the Kubernetes exec API.
// This function handles the complex SPDY streaming protocol and provides proper error classification.
func execShell(ctx context.Context, cfg *rest.Config, cs *kubernetes.Clientset, pod *corev1.Pod, container string) (string, error) {
	req := cs.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{"/bin/sh", "-c", "nvidia-smi -q -x"},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false, // Disable TTY for proper stdout/stderr separation
		}, scheme.ParameterCodec)

	// Create the SPDY executor for streaming communication
	executor, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	// Capture stdout and stderr in separate buffers
	var stdout, stderr bytes.Buffer

	// Execute the command with context cancellation support
	// The context allows for proper timeout handling and cancellation
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	// Convert outputs to strings for easier handling
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	// Handle stderr output (command executed but produced errors)
	if stderrStr != "" {
		return "", fmt.Errorf("command stderr: %s", stderrStr)
	}

	// Handle success case (exit code 0)
	if err == nil {
		return stdoutStr, nil
	}

	// Classify the error type for better error handling
	var exitError utilexec.ExitError
	if errors.As(err, &exitError) {
		// Command executed but returned non-zero exit code
		return stdoutStr, fmt.Errorf("command failed with exit code %d: %w", exitError.ExitStatus(), err)
	}

	// Network/transport error (connection issues, timeouts, etc.)
	return stdoutStr, fmt.Errorf("execution stream error: %w", err)
}
