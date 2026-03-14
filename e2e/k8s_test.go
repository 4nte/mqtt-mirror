//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	corev1 "k8s.io/api/core/v1"
)

const (
	brokerUser = "testuser"
	brokerPass = "testpassword"
)

func e2eHelmValues() map[string]interface{} {
	return map[string]interface{}{
		"mqtt": map[string]interface{}{
			"source": fmt.Sprintf("tcp://%s:%s@mqtt-source:1883", brokerUser, brokerPass),
			"target": fmt.Sprintf("tcp://%s:%s@mqtt-target:1883", brokerUser, brokerPass),
		},
		"image": map[string]interface{}{
			"repository": "mqtt-mirror",
			"tag":        "e2e-test",
			"pullPolicy": "Never",
		},
	}
}

func TestHelmTemplate_RendersValidManifests(t *testing.T) {
	values := map[string]interface{}{
		"mqtt": map[string]interface{}{
			"source": "tcp://mqtt-source:1883",
			"target": "tcp://mqtt-target:1883",
		},
		"image": map[string]interface{}{
			"repository": "mqtt-mirror",
			"tag":        "e2e-test",
			"pullPolicy": "Never",
		},
	}

	manifest := helmTemplate(t, "test-render", values)

	missing := containsAll(manifest,
		"kind: Deployment",
		"/healthz",
		"/readyz",
		"containerPort: 8080",
		"tcp://mqtt-source:1883",
		"tcp://mqtt-target:1883",
	)
	require.Empty(t, missing, "rendered manifest missing expected strings: %v", missing)
}

func TestHelmTemplate_RequiresSourceAndTarget(t *testing.T) {
	t.Run("missing source", func(t *testing.T) {
		values := map[string]interface{}{
			"mqtt": map[string]interface{}{
				"target": "tcp://mqtt-target:1883",
			},
		}
		root := projectRoot()
		chartPath := root + "/charts/mqtt-mirror"

		settings := cli.New()
		actionConfig := new(action.Configuration)
		_ = actionConfig.Init(settings.RESTClientGetter(), "default", "memory", func(format string, v ...interface{}) {})

		chart, err := loader.Load(chartPath)
		require.NoError(t, err)

		install := action.NewInstall(actionConfig)
		install.ReleaseName = "test-missing-source"
		install.Namespace = "default"
		install.ClientOnly = true
		install.DryRun = true

		_, err = install.Run(chart, values)
		require.Error(t, err, "should fail when mqtt.source is empty")
	})

	t.Run("missing target", func(t *testing.T) {
		values := map[string]interface{}{
			"mqtt": map[string]interface{}{
				"source": "tcp://mqtt-source:1883",
			},
		}
		root := projectRoot()
		chartPath := root + "/charts/mqtt-mirror"

		settings := cli.New()
		actionConfig := new(action.Configuration)
		_ = actionConfig.Init(settings.RESTClientGetter(), "default", "memory", func(format string, v ...interface{}) {})

		chart, err := loader.Load(chartPath)
		require.NoError(t, err)

		install := action.NewInstall(actionConfig)
		install.ReleaseName = "test-missing-target"
		install.Namespace = "default"
		install.ClientOnly = true
		install.DryRun = true

		_, err = install.Run(chart, values)
		require.Error(t, err, "should fail when mqtt.target is empty")
	})
}

func TestHelmInstall_PodBecomesReady(t *testing.T) {
	setupCluster(t)

	releaseName := "e2e-ready"
	values := e2eHelmValues()

	helmInstall(t, releaseName, values)
	t.Cleanup(func() { helmUninstall(t, releaseName) })

	deployName := releaseName + "-mqtt-mirror"
	err := waitForDeploymentReady(kubeClient, "default", deployName, 120*time.Second)
	if err != nil {
		// Print debug info on failure
		t.Logf("Deployment describe:\n%s", kubectlDescribe(t, "deployment", deployName))
		pods := getPods(t, "default", "app.kubernetes.io/instance="+releaseName)
		for _, pod := range pods {
			t.Logf("Pod %s phase=%s", pod.Name, pod.Status.Phase)
			for _, cs := range pod.Status.ContainerStatuses {
				t.Logf("  container=%s ready=%v restarts=%d", cs.Name, cs.Ready, cs.RestartCount)
				if cs.State.Waiting != nil {
					t.Logf("  waiting: reason=%s", cs.State.Waiting.Reason)
				}
			}
			t.Logf("  logs:\n%s", getPodLogs(t, "default", pod.Name))

			// DNS and connectivity debug from within the pod
			dnsOut, _ := exec.Command("kubectl", "exec", pod.Name, "--kubeconfig", kubeconfigPath, "-n", "default", "--", "nslookup", "mqtt-source").CombinedOutput()
			t.Logf("  nslookup mqtt-source:\n%s", dnsOut)
		}

		epOut, _ := exec.Command("kubectl", "get", "endpoints", "-n", "default", "--kubeconfig", kubeconfigPath).CombinedOutput()
		t.Logf("Endpoints:\n%s", epOut)
		events, _ := exec.Command("kubectl", "get", "events", "-n", "default", "--sort-by=.lastTimestamp", "--kubeconfig", kubeconfigPath).CombinedOutput()
		t.Logf("Events:\n%s", events)

		require.NoError(t, err)
	}

	dep := getDeployment(t, "default", deployName)
	require.GreaterOrEqual(t, dep.Status.ReadyReplicas, int32(1), "expected at least 1 ready replica")

	pods := getPods(t, "default", "app.kubernetes.io/instance="+releaseName)
	require.NotEmpty(t, pods, "expected at least 1 pod")

	pod := pods[0]
	require.Equal(t, corev1.PodRunning, pod.Status.Phase, "pod should be Running")

	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.ContainersReady {
			require.Equal(t, corev1.ConditionTrue, cond.Status, "ContainersReady should be True")
		}
	}
}

func TestLivenessProbe_NoRestarts(t *testing.T) {
	setupCluster(t)

	releaseName := "e2e-liveness"
	values := e2eHelmValues()

	helmInstall(t, releaseName, values)
	t.Cleanup(func() { helmUninstall(t, releaseName) })

	deployName := releaseName + "-mqtt-mirror"
	err := waitForDeploymentReady(kubeClient, "default", deployName, 120*time.Second)
	require.NoError(t, err)

	// Wait extra time to allow liveness probes to run (period=10s, so wait ~25s)
	time.Sleep(25 * time.Second)

	pods := getPods(t, "default", "app.kubernetes.io/instance="+releaseName)
	require.NotEmpty(t, pods)

	for _, cs := range pods[0].Status.ContainerStatuses {
		require.Equal(t, int32(0), cs.RestartCount,
			"container %s should have 0 restarts, got %d", cs.Name, cs.RestartCount)
	}
}

func TestE2E_MessageMirroring(t *testing.T) {
	setupCluster(t)

	releaseName := "e2e-mirror"
	values := e2eHelmValues()

	helmInstall(t, releaseName, values)
	t.Cleanup(func() { helmUninstall(t, releaseName) })

	deployName := releaseName + "-mqtt-mirror"
	err := waitForDeploymentReady(kubeClient, "default", deployName, 120*time.Second)
	require.NoError(t, err)

	// Port-forward both broker services
	sourcePort, cancelSource := portForward(t, "mqtt-source", 1883)
	t.Cleanup(cancelSource)

	targetPort, cancelTarget := portForward(t, "mqtt-target", 1883)
	t.Cleanup(cancelTarget)

	sourceBroker := fmt.Sprintf("tcp://127.0.0.1:%d", sourcePort)
	targetBroker := fmt.Sprintf("tcp://127.0.0.1:%d", targetPort)

	// Subscribe on target
	mu := sync.Mutex{}
	var messages []paho.Message

	targetOpts := paho.NewClientOptions().
		AddBroker(targetBroker).
		SetAutoReconnect(true).
		SetUsername(brokerUser).
		SetPassword(brokerPass).
		SetClientID("e2e-target-sub")
	targetClient := paho.NewClient(targetOpts)
	token := targetClient.Connect()
	require.True(t, token.WaitTimeout(30*time.Second), "target client connect timeout")
	require.NoError(t, token.Error())
	defer targetClient.Disconnect(250)

	token = targetClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, msg)
	})
	token.Wait()
	require.NoError(t, token.Error())

	// Give the mirror a moment to establish subscriptions
	time.Sleep(2 * time.Second)

	// Publish on source
	sourceOpts := paho.NewClientOptions().
		AddBroker(sourceBroker).
		SetAutoReconnect(true).
		SetUsername(brokerUser).
		SetPassword(brokerPass).
		SetClientID("e2e-source-pub")
	sourceClient := paho.NewClient(sourceOpts)
	token = sourceClient.Connect()
	require.True(t, token.WaitTimeout(30*time.Second), "source client connect timeout")
	require.NoError(t, token.Error())
	defer sourceClient.Disconnect(250)

	testPayload := []byte("e2e-test-message")
	testTopic := "e2e/mirror/test"

	token = sourceClient.Publish(testTopic, byte(0), false, testPayload)
	token.Wait()
	require.NoError(t, token.Error())

	// Wait for message to arrive on target
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(messages)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(messages), 1, "expected at least 1 mirrored message on target broker")
	require.Equal(t, testTopic, messages[0].Topic())
	require.Equal(t, testPayload, messages[0].Payload())
}
