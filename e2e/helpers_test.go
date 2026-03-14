//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	dcontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	k3s "github.com/testcontainers/testcontainers-go/modules/k3s"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	setupOnce      sync.Once
	setupErr       error
	k3sContainer   *k3s.K3sContainer
	kubeClient     *kubernetes.Clientset
	kubeconfigPath string
)

func projectRoot() string {
	_, b, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(b))
}

func setupCluster(t *testing.T) {
	t.Helper()
	setupOnce.Do(func() {
		setupErr = doSetupCluster()
	})
	require.NoError(t, setupErr, "cluster setup failed")
}

func doSetupCluster() error {
	ctx := context.Background()
	root := projectRoot()

	// 1. Cross-compile the binary for linux/amd64
	buildCmd := exec.Command("go", "build",
		"-ldflags=-X github.com/4nte/mqtt-mirror/cmd.version=e2e-test",
		"-o", filepath.Join(root, "mqtt-mirror"),
		filepath.Join(root, "main.go"),
	)
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %w\n%s", err, out)
	}

	// 2. Build Docker image
	dockerBuild := exec.Command("docker", "build", "-t", "mqtt-mirror:e2e-test", "-f", filepath.Join(root, "Dockerfile"), root)
	if out, err := dockerBuild.CombinedOutput(); err != nil {
		return fmt.Errorf("docker build failed: %w\n%s", err, out)
	}

	// 3. Start K3s container with a tmpfs-backed data directory.
	// Without this, K3s reports "invalid capacity 0 on image filesystem" and
	// taints the node with disk-pressure, preventing pod scheduling.
	// Note: WithHostConfigModifier replaces (not appends), so we must
	// replicate the K3s module's defaults alongside our tmpfs addition.
	var err error
	k3sContainer, err = k3s.Run(ctx, "rancher/k3s:v1.31.6-k3s1",
		testcontainers.WithHostConfigModifier(func(hc *dcontainer.HostConfig) {
			hc.Privileged = true
			hc.CgroupnsMode = "host"
			hc.Tmpfs = map[string]string{
				"/run":                 "",
				"/var/run":             "",
				"/var/lib/rancher/k3s": "size=4g",
			}
			hc.Mounts = []mount.Mount{}
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to start k3s: %w", err)
	}

	// 4. Load images into K3s
	if err := k3sContainer.LoadImages(ctx, "mqtt-mirror:e2e-test"); err != nil {
		return fmt.Errorf("failed to load mqtt-mirror image into k3s: %w", err)
	}

	pullCmd := exec.Command("docker", "pull", "docker.io/volantmq/volantmq:v0.4.0-rc.8")
	if out, err := pullCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker pull volantmq failed: %w\n%s", err, out)
	}
	if err := k3sContainer.LoadImages(ctx, "docker.io/volantmq/volantmq:v0.4.0-rc.8"); err != nil {
		return fmt.Errorf("failed to load volantmq image into k3s: %w", err)
	}

	// 5. Extract kubeconfig
	kubeConfigYaml, err := k3sContainer.GetKubeConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "kubeconfig-e2e-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp kubeconfig: %w", err)
	}
	if _, err := tmpFile.Write(kubeConfigYaml); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}
	tmpFile.Close()
	kubeconfigPath = tmpFile.Name()

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigYaml)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	kubeClient, err = kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	// 6. Wait for CoreDNS to be ready (required for in-cluster DNS resolution)
	if err := waitForDeploymentReady(kubeClient, "kube-system", "coredns", 120*time.Second); err != nil {
		return fmt.Errorf("coredns not ready: %w", err)
	}

	// 7. Apply volantmq manifests
	manifestPath := filepath.Join(root, "e2e", "testdata", "volantmq.yaml")
	applyCmd := exec.Command("kubectl", "apply", "-f", manifestPath, "--kubeconfig", kubeconfigPath)
	if out, err := applyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w\n%s", err, out)
	}

	// 8. Wait for broker deployments to be ready
	if err := waitForDeploymentReady(kubeClient, "default", "mqtt-source", 120*time.Second); err != nil {
		return fmt.Errorf("mqtt-source not ready: %w\n%s", err, debugClusterState(kubeClient, kubeconfigPath, "mqtt-source"))
	}
	if err := waitForDeploymentReady(kubeClient, "default", "mqtt-target", 120*time.Second); err != nil {
		return fmt.Errorf("mqtt-target not ready: %w\n%s", err, debugClusterState(kubeClient, kubeconfigPath, "mqtt-target"))
	}

	return nil
}

func debugClusterState(clientset *kubernetes.Clientset, kubeconfig, deployName string) string {
	ctx := context.Background()
	var sb strings.Builder

	// Describe deployment
	descCmd := exec.Command("kubectl", "describe", "deployment", deployName,
		"--kubeconfig", kubeconfig, "-n", "default")
	if out, err := descCmd.CombinedOutput(); err == nil {
		sb.WriteString(fmt.Sprintf("=== kubectl describe deployment %s ===\n%s\n", deployName, out))
	}

	// Get pod status and events
	pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + deployName,
	})
	if err == nil {
		for _, pod := range pods.Items {
			sb.WriteString(fmt.Sprintf("=== Pod %s phase=%s ===\n", pod.Name, pod.Status.Phase))
			for _, cs := range pod.Status.ContainerStatuses {
				sb.WriteString(fmt.Sprintf("  container=%s ready=%v restarts=%d\n", cs.Name, cs.Ready, cs.RestartCount))
				if cs.State.Waiting != nil {
					sb.WriteString(fmt.Sprintf("  waiting: reason=%s message=%s\n", cs.State.Waiting.Reason, cs.State.Waiting.Message))
				}
				if cs.State.Terminated != nil {
					sb.WriteString(fmt.Sprintf("  terminated: reason=%s exitCode=%d\n", cs.State.Terminated.Reason, cs.State.Terminated.ExitCode))
				}
			}
			// Pod logs
			req := clientset.CoreV1().Pods("default").GetLogs(pod.Name, &corev1.PodLogOptions{TailLines: int64Ptr(50)})
			if stream, err := req.Stream(ctx); err == nil {
				buf, _ := io.ReadAll(stream)
				stream.Close()
				sb.WriteString(fmt.Sprintf("  logs:\n%s\n", buf))
			}
		}
	}

	// Events
	eventsCmd := exec.Command("kubectl", "get", "events", "--sort-by=.lastTimestamp",
		"--kubeconfig", kubeconfig, "-n", "default")
	if out, err := eventsCmd.CombinedOutput(); err == nil {
		sb.WriteString(fmt.Sprintf("=== Events ===\n%s\n", out))
	}

	return sb.String()
}

func int64Ptr(i int64) *int64 { return &i }

func waitForDeploymentReady(clientset *kubernetes.Clientset, namespace, name string, timeout time.Duration) error {
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && dep.Status.ReadyReplicas >= 1 {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("deployment %s/%s not ready after %s", namespace, name, timeout)
}

func helmInstall(t *testing.T, releaseName string, values map[string]interface{}) {
	t.Helper()
	root := projectRoot()
	chartPath := filepath.Join(root, "charts", "mqtt-mirror")

	settings := cli.New()
	settings.KubeConfig = kubeconfigPath

	actionConfig := new(action.Configuration)
	err := actionConfig.Init(settings.RESTClientGetter(), "default", "secret", func(format string, v ...interface{}) {
		t.Logf(format, v...)
	})
	require.NoError(t, err, "helm action config init failed")

	chart, err := loader.Load(chartPath)
	require.NoError(t, err, "failed to load chart")

	install := action.NewInstall(actionConfig)
	install.ReleaseName = releaseName
	install.Namespace = "default"
	install.Wait = false
	install.Timeout = 120 * time.Second

	_, err = install.Run(chart, values)
	require.NoError(t, err, "helm install failed")
}

func helmUninstall(t *testing.T, releaseName string) {
	t.Helper()

	settings := cli.New()
	settings.KubeConfig = kubeconfigPath

	actionConfig := new(action.Configuration)
	err := actionConfig.Init(settings.RESTClientGetter(), "default", "secret", func(format string, v ...interface{}) {
		t.Logf(format, v...)
	})
	if err != nil {
		t.Logf("helm uninstall config init failed (non-fatal): %v", err)
		return
	}

	uninstall := action.NewUninstall(actionConfig)
	if _, err = uninstall.Run(releaseName); err != nil {
		t.Logf("helm uninstall %s failed (non-fatal): %v", releaseName, err)
	}
}

func helmTemplate(t *testing.T, releaseName string, values map[string]interface{}) string {
	t.Helper()
	root := projectRoot()
	chartPath := filepath.Join(root, "charts", "mqtt-mirror")

	settings := cli.New()

	actionConfig := new(action.Configuration)
	err := actionConfig.Init(settings.RESTClientGetter(), "default", "memory", func(format string, v ...interface{}) {
		t.Logf(format, v...)
	})
	require.NoError(t, err, "helm action config init failed")

	chart, err := loader.Load(chartPath)
	require.NoError(t, err, "failed to load chart")

	install := action.NewInstall(actionConfig)
	install.ReleaseName = releaseName
	install.Namespace = "default"
	install.ClientOnly = true
	install.DryRun = true

	rel, err := install.Run(chart, values)
	require.NoError(t, err, "helm template failed")
	return rel.Manifest
}

func getDeployment(t *testing.T, namespace, name string) *appsv1.Deployment {
	t.Helper()
	dep, err := kubeClient.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err, "failed to get deployment %s/%s", namespace, name)
	return dep
}

func getPods(t *testing.T, namespace string, labelSelector string) []corev1.Pod {
	t.Helper()
	pods, err := kubeClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	require.NoError(t, err, "failed to list pods")
	return pods.Items
}

// portForward starts kubectl port-forward for a service and returns the local port and a cancel func.
func portForward(t *testing.T, svcName string, remotePort int) (int, func()) {
	t.Helper()

	localPort := getFreePort(t)

	cmd := exec.Command("kubectl", "port-forward",
		fmt.Sprintf("svc/%s", svcName),
		fmt.Sprintf("%d:%d", localPort, remotePort),
		"--kubeconfig", kubeconfigPath,
		"-n", "default",
	)

	// Capture stderr for debugging
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Start()
	require.NoError(t, err, "failed to start port-forward: %s", stderr.String())

	cancel := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}

	// Wait for port to become available
	waitForTCP(t, fmt.Sprintf("127.0.0.1:%d", localPort), 30*time.Second)

	return localPort, cancel
}

func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "failed to get free port")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func waitForTCP(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("tcp endpoint %s not ready after %s", addr, timeout)
}

func getPodLogs(t *testing.T, namespace, podName string) string {
	t.Helper()
	req := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	stream, err := req.Stream(context.Background())
	if err != nil {
		return fmt.Sprintf("failed to get logs: %v", err)
	}
	defer stream.Close()
	buf, _ := io.ReadAll(stream)
	return string(buf)
}

func kubectlDescribe(t *testing.T, resource, name string) string {
	t.Helper()
	cmd := exec.Command("kubectl", "describe", resource, name,
		"--kubeconfig", kubeconfigPath, "-n", "default")
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// containsAll checks that s contains all of the given substrings.
func containsAll(s string, substrs ...string) []string {
	var missing []string
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			missing = append(missing, sub)
		}
	}
	return missing
}
