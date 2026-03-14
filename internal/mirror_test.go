package internal

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"sync"

	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type MqttBroker struct {
	nat.PortBinding
	Terminate func()
	container testcontainers.Container
}

func (b MqttBroker) Uri() string {
	return fmt.Sprintf("tcp://%s:%s", b.HostIP, b.HostPort)
}

func NewMQTTContainer(requireAuth bool, hostPort ...string) (MqttBroker, error) {
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(b)

	var configFile string
	if requireAuth {
		configFile = "volantmq-config.yaml"
	} else {
		configFile = "volantmq-config-no-auth.yaml"
	}

	exposedPort := "1883/tcp"
	if len(hostPort) > 0 && hostPort[0] != "" {
		exposedPort = hostPort[0] + ":1883/tcp"
	}

	configFilePath := path.Join(basepath, configFile)
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "docker.io/volantmq/volantmq:v0.4.0-rc.8",
		ExposedPorts: []string{exposedPort},
		WaitingFor:   wait.ForLog("listener state: id: :1883 status: started"),
		Env: map[string]string{
			"VOLANTMQ_CONFIG": "/etc/volantmq-config.yaml",
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      configFilePath,
				ContainerFilePath: "/etc/volantmq-config.yaml",
				FileMode:          0o644,
			},
		},
	}
	brokerContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Reuse:            false,
	})
	if err != nil {
		return MqttBroker{}, err
	}
	host, err := brokerContainer.Host(ctx)
	if err != nil {
		return MqttBroker{}, err
	}

	containerPort, err := nat.NewPort("tcp", "1883")
	if err != nil {
		return MqttBroker{}, err
	}
	port, err := brokerContainer.MappedPort(ctx, containerPort)
	if err != nil {
		return MqttBroker{}, err
	}

	return MqttBroker{
		PortBinding: nat.PortBinding{
			HostIP:   host,
			HostPort: strings.Split(string(port), "/")[0],
		},
		Terminate: func() {
			_ = brokerContainer.Terminate(context.Background())
		},
		container: brokerContainer,
	}, nil
}

func NewClient(t *testing.T, broker string, username string, password string, clientID string) paho.Client {
	t.Helper()
	clientOpts := paho.NewClientOptions().AddBroker(broker).SetAutoReconnect(true).SetMaxReconnectInterval(30 * time.Second).SetUsername(username).SetPassword(password).SetClientID(clientID)

	clientOpts.SetOnConnectHandler(func(client paho.Client) {
		fmt.Printf("connection established to %s (%s)\n", broker, clientID)
	})
	clientOpts.SetConnectionLostHandler(func(i paho.Client, error error) {
		fmt.Print(fmt.Errorf("connection lost with %s (%s)\n", broker, clientID))
	})

	client := paho.NewClient(clientOpts)

	token := client.Connect()
	connTimeout := 30 * time.Second
	ok := token.WaitTimeout(connTimeout)
	require.True(t, ok, "connection timeout exceeded (%s): %s (%s)", connTimeout, broker, clientID)

	return client
}

func TestMirror_withAuth(t *testing.T) {
	sourceBroker, err := NewMQTTContainer(true)
	if err != nil {
		t.Fatalf("failed to start a source broker: %s", err)
	}
	destinationBroker, err := NewMQTTContainer(true)
	if err != nil {
		t.Fatalf("failed to start a source broker: %s", err)
	}

	// MQTT credentials for both brokers
	username := "testuser"
	password := "testpassword"

	// Start Mirror func
	sourceURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", username, password, sourceBroker.HostIP, sourceBroker.HostPort))
	require.NoError(t, err)
	destinationURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", username, password, destinationBroker.HostIP, destinationBroker.HostPort))
	require.NoError(t, err)

	terminateMirror, err := Mirror(*sourceURL, *destinationURL, []string{}, true, 0, "", true, nil, nil)
	require.NoError(t, err)

	mutex := sync.Mutex{}

	defer sourceBroker.Terminate()
	defer destinationBroker.Terminate()

	var sourceBrokerMessages []paho.Message
	var destinationBrokerMessages []paho.Message

	fmt.Println(sourceBroker.Uri())
	// Create client and subscribe to all topics
	sourceBrokerClient := NewClient(t, sourceBroker.Uri(), username, password, "source-client")
	token := sourceBrokerClient.Subscribe("#", byte(1), func(client paho.Client, msg paho.Message) {
		mutex.Lock()
		defer mutex.Unlock()
		sourceBrokerMessages = append(sourceBrokerMessages, msg)
	})
	token.Wait()

	// Create client and subscribe to all topics
	destinationBrokerClient := NewClient(t, destinationBroker.Uri(), username, password, "destination-client")
	token = destinationBrokerClient.Subscribe("#", byte(1), func(client paho.Client, msg paho.Message) {
		mutex.Lock()
		defer mutex.Unlock()
		destinationBrokerMessages = append(destinationBrokerMessages, msg)
	})
	token.Wait()

	token = sourceBrokerClient.Publish("test/msg1", byte(1), false, []byte("foo"))
	token.Wait()

	token = sourceBrokerClient.Publish("test/msg2", byte(1), false, []byte("foo"))
	token.Wait()

	token = sourceBrokerClient.Publish("test/msg3", byte(1), false, []byte("foo"))
	token.Wait()

	<-time.After(1 * time.Second)
	terminateMirror()

	require.Lenf(t, sourceBrokerMessages, 3, "Source broker should have 3 messages")
	require.Lenf(t, destinationBrokerMessages, 3, "destination broker should have 3 messages")
	for _, sourceMessage := range sourceBrokerMessages {
		found := slices.ContainsFunc(destinationBrokerMessages, func(msg paho.Message) bool {
			return string(sourceMessage.Payload()) == string(msg.Payload())
		})
		require.True(t, found, "message not duplicated")
	}
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

func TestMirror_reconnect(t *testing.T) {
	// Use a fixed host port so the port mapping survives container restart
	sourceBroker, err := NewMQTTContainer(true, "21883")
	require.NoError(t, err, "failed to start source broker")
	defer sourceBroker.Terminate()

	destinationBroker, err := NewMQTTContainer(true)
	require.NoError(t, err, "failed to start destination broker")
	defer destinationBroker.Terminate()

	username := "testuser"
	password := "testpassword"

	sourceURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", username, password, sourceBroker.HostIP, sourceBroker.HostPort))
	require.NoError(t, err)
	destinationURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", username, password, destinationBroker.HostIP, destinationBroker.HostPort))
	require.NoError(t, err)

	terminateMirror, err := Mirror(*sourceURL, *destinationURL, []string{}, true, 0, "", true, nil, nil)
	require.NoError(t, err)
	defer terminateMirror()

	mutex := sync.Mutex{}
	var destinationMessages []paho.Message

	// Subscribe on destination to verify mirrored messages
	destClient := NewClient(t, destinationBroker.Uri(), username, password, "dest-sub")
	token := destClient.Subscribe("#", byte(1), func(client paho.Client, msg paho.Message) {
		mutex.Lock()
		defer mutex.Unlock()
		destinationMessages = append(destinationMessages, msg)
	})
	token.Wait()

	// Phase 1: Publish before restart — verify baseline mirroring works
	srcClient := NewClient(t, sourceBroker.Uri(), username, password, "src-pub")
	token = srcClient.Publish("test/before", byte(1), false, []byte("before-restart"))
	token.Wait()

	time.Sleep(2 * time.Second)
	mutex.Lock()
	require.Len(t, destinationMessages, 1, "baseline: message should be mirrored before restart")
	mutex.Unlock()

	// Phase 2: Restart the source broker to simulate disconnection
	ctx := context.Background()
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err, "failed to create docker client")
	defer func() { _ = dockerClient.Close() }()

	containerID := sourceBroker.container.GetContainerID()
	stopTimeout := 5
	err = dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTimeout})
	require.NoError(t, err, "failed to stop source broker")

	time.Sleep(2 * time.Second)

	err = dockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
	require.NoError(t, err, "failed to restart source broker")

	// Wait for the MQTT broker inside the container to accept TCP connections
	brokerAddr := fmt.Sprintf("%s:%s", sourceBroker.HostIP, sourceBroker.HostPort)
	waitForTCP(t, brokerAddr, 30*time.Second)

	// Phase 3: Publish after restart — verify mirroring resumed.
	// Disconnect old test publisher and wait for the broker to accept MQTT
	// connections (TCP being open is not enough — the MQTT handler needs time).
	srcClient.Disconnect(0)
	var srcClient2 paho.Client
	for range 15 {
		clientOpts := paho.NewClientOptions().AddBroker(sourceBroker.Uri()).SetAutoReconnect(true).SetMaxReconnectInterval(30 * time.Second).SetUsername(username).SetPassword(password).SetClientID("src-pub2")
		c := paho.NewClient(clientOpts)
		token := c.Connect()
		if token.WaitTimeout(5*time.Second) && token.Error() == nil && c.IsConnected() {
			srcClient2 = c
			break
		}
		time.Sleep(time.Second)
	}
	require.NotNil(t, srcClient2, "failed to connect test publisher after broker restart")
	require.True(t, srcClient2.IsConnected(), "test publisher not connected")

	// Poll: publish and wait for the message to be mirrored.
	// Paho's auto-reconnect uses exponential backoff (up to 15s),
	// so the mirror may take up to ~30s to reconnect and re-subscribe.
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		token = srcClient2.Publish("test/after", byte(1), false, []byte("after-restart"))
		token.Wait()

		time.Sleep(3 * time.Second)

		mutex.Lock()
		count := len(destinationMessages)
		mutex.Unlock()
		if count >= 2 {
			break
		}
	}

	mutex.Lock()
	require.GreaterOrEqual(t, len(destinationMessages), 2, "after reconnect: message should be mirrored after broker restart")
	mutex.Unlock()
}

// waitForMessages polls until at least minCount messages have been collected, or the timeout expires.
func waitForMessages(t *testing.T, mu *sync.Mutex, messages *[]paho.Message, minCount int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(*messages)
		mu.Unlock()
		if n >= minCount {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("timed out waiting for %d messages, got %d after %s", minCount, len(*messages), timeout)
}

const testUsername = "testuser"
const testPassword = "testpassword"

// setupMirror creates two auth brokers and starts a mirror between them.
// Returns the brokers, a terminate function, and a cleanup function.
func setupMirror(t *testing.T, topics []string) (MqttBroker, MqttBroker, func()) {
	t.Helper()
	sourceBroker, err := NewMQTTContainer(true)
	require.NoError(t, err, "failed to start source broker")

	destinationBroker, err := NewMQTTContainer(true)
	require.NoError(t, err, "failed to start destination broker")

	sourceURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", testUsername, testPassword, sourceBroker.HostIP, sourceBroker.HostPort))
	require.NoError(t, err)
	destinationURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", testUsername, testPassword, destinationBroker.HostIP, destinationBroker.HostPort))
	require.NoError(t, err)

	terminateMirror, err := Mirror(*sourceURL, *destinationURL, topics, false, 0, "test", true, nil, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		terminateMirror()
		sourceBroker.Terminate()
		destinationBroker.Terminate()
	})

	// Give mirror a moment to establish subscriptions
	time.Sleep(1 * time.Second)

	return sourceBroker, destinationBroker, terminateMirror
}

func TestMirror_RetainFlag(t *testing.T) {
	sourceBroker, destinationBroker, _ := setupMirror(t, nil)

	srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-retain")

	// Publish with retain=true
	token := srcClient.Publish("retain/test", byte(0), true, []byte("retained-msg"))
	token.Wait()

	// Wait for mirror to forward
	time.Sleep(2 * time.Second)

	// Connect a NEW subscriber on destination — new subs see retained messages
	mu := sync.Mutex{}
	var messages []paho.Message
	destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-retain")
	token = destClient.Subscribe("retain/#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, msg)
	})
	token.Wait()

	waitForMessages(t, &mu, &messages, 1, 5*time.Second)

	mu.Lock()
	require.True(t, messages[0].Retained(), "expected retained flag to be preserved")
	mu.Unlock()

	// Now publish with retain=false
	mu.Lock()
	messages = nil
	mu.Unlock()

	token = srcClient.Publish("retain/test2", byte(0), false, []byte("not-retained"))
	token.Wait()

	waitForMessages(t, &mu, &messages, 1, 5*time.Second)

	mu.Lock()
	require.False(t, messages[0].Retained(), "expected retained flag to be false")
	mu.Unlock()
}

func TestMirror_QoSPreservation(t *testing.T) {
	// Although the mirror subscribes at QoS 0, the message handler forwards
	// messages with the original publish QoS (via message.Qos()). This test
	// verifies that the QoS level is preserved end-to-end.
	for _, publishQoS := range []byte{0, 1, 2} {
		t.Run(fmt.Sprintf("publish_qos_%d", publishQoS), func(t *testing.T) {
			sourceBroker, destinationBroker, _ := setupMirror(t, nil)

			mu := sync.Mutex{}
			var messages []paho.Message
			destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-qos")
			token := destClient.Subscribe("#", byte(2), func(client paho.Client, msg paho.Message) {
				mu.Lock()
				defer mu.Unlock()
				messages = append(messages, msg)
			})
			token.Wait()

			srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-qos")
			token = srcClient.Publish("qos/test", publishQoS, false, []byte("qos-msg"))
			token.Wait()

			waitForMessages(t, &mu, &messages, 1, 5*time.Second)

			mu.Lock()
			require.Equal(t, publishQoS, messages[0].Qos(),
				"expected QoS %d on destination, got QoS %d", publishQoS, messages[0].Qos())
			mu.Unlock()
		})
	}
}

func TestMirror_PayloadEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"empty payload", []byte{}},
		{"binary payload", []byte{0x00, 0xFF, 0x01, 0xFE}},
		{"large payload 64KB", bytes.Repeat([]byte("x"), 64*1024)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceBroker, destinationBroker, _ := setupMirror(t, nil)

			mu := sync.Mutex{}
			var messages []paho.Message
			destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-pay")
			token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
				mu.Lock()
				defer mu.Unlock()
				messages = append(messages, msg)
			})
			token.Wait()

			srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-pay")
			token = srcClient.Publish("payload/test", byte(0), false, tt.payload)
			token.Wait()

			waitForMessages(t, &mu, &messages, 1, 5*time.Second)

			mu.Lock()
			require.True(t, bytes.Equal(messages[0].Payload(), tt.payload),
				"payload mismatch: got %d bytes, want %d bytes", len(messages[0].Payload()), len(tt.payload))
			mu.Unlock()
		})
	}
}

func TestMirror_SpecialTopics(t *testing.T) {
	tests := []struct {
		name  string
		topic string
	}{
		{"deep nesting", "a/b/c/d/e/f/g/h"},
		{"single char", "x"},
		{"numeric", "123/456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceBroker, destinationBroker, _ := setupMirror(t, nil)

			mu := sync.Mutex{}
			var messages []paho.Message
			destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-top")
			token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
				mu.Lock()
				defer mu.Unlock()
				messages = append(messages, msg)
			})
			token.Wait()

			srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-top")
			token = srcClient.Publish(tt.topic, byte(0), false, []byte("topic-test"))
			token.Wait()

			waitForMessages(t, &mu, &messages, 1, 5*time.Second)

			mu.Lock()
			require.Equal(t, tt.topic, messages[0].Topic(), "topic should be preserved exactly")
			mu.Unlock()
		})
	}
}

func TestMirror_TopicFiltering(t *testing.T) {
	sourceBroker, destinationBroker, _ := setupMirror(t, []string{"allowed/topic", "other/allowed"})

	mu := sync.Mutex{}
	var messages []paho.Message
	destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-filt")
	token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, msg)
	})
	token.Wait()

	srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-filt")

	token = srcClient.Publish("allowed/topic", byte(0), false, []byte("yes1"))
	token.Wait()
	token = srcClient.Publish("blocked/topic", byte(0), false, []byte("no"))
	token.Wait()
	token = srcClient.Publish("other/allowed", byte(0), false, []byte("yes2"))
	token.Wait()

	// Wait for forwarded messages
	waitForMessages(t, &mu, &messages, 2, 5*time.Second)

	// Wait extra to ensure blocked message doesn't arrive
	time.Sleep(3 * time.Second)

	mu.Lock()
	require.Len(t, messages, 2, "expected exactly 2 forwarded messages")
	for _, msg := range messages {
		require.NotEqual(t, "blocked/topic", msg.Topic(), "blocked topic should not be forwarded")
	}
	mu.Unlock()
}

func TestMirror_WildcardPlus(t *testing.T) {
	sourceBroker, destinationBroker, _ := setupMirror(t, []string{"sensors/+/temperature"})

	mu := sync.Mutex{}
	var messages []paho.Message
	destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-wc-p")
	token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, msg)
	})
	token.Wait()

	srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-wc-p")

	token = srcClient.Publish("sensors/room1/temperature", byte(0), false, []byte("match1"))
	token.Wait()
	token = srcClient.Publish("sensors/room2/temperature", byte(0), false, []byte("match2"))
	token.Wait()
	token = srcClient.Publish("sensors/room1/humidity", byte(0), false, []byte("nomatch"))
	token.Wait()

	waitForMessages(t, &mu, &messages, 2, 5*time.Second)
	time.Sleep(3 * time.Second)

	mu.Lock()
	require.Len(t, messages, 2, "expected exactly 2 messages matching sensors/+/temperature")
	mu.Unlock()
}

func TestMirror_WildcardHash(t *testing.T) {
	sourceBroker, destinationBroker, _ := setupMirror(t, []string{"home/#"})

	mu := sync.Mutex{}
	var messages []paho.Message
	destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-wc-h")
	token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, msg)
	})
	token.Wait()

	srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-wc-h")

	token = srcClient.Publish("home/living/light", byte(0), false, []byte("match1"))
	token.Wait()
	token = srcClient.Publish("home/kitchen", byte(0), false, []byte("match2"))
	token.Wait()
	token = srcClient.Publish("office/desk", byte(0), false, []byte("nomatch"))
	token.Wait()

	waitForMessages(t, &mu, &messages, 2, 5*time.Second)
	time.Sleep(3 * time.Second)

	mu.Lock()
	require.Len(t, messages, 2, "expected exactly 2 messages matching home/#")
	mu.Unlock()
}

func TestMirror_TargetBrokerReconnect(t *testing.T) {
	sourceBroker, err := NewMQTTContainer(true)
	require.NoError(t, err, "failed to start source broker")
	defer sourceBroker.Terminate()

	// Use fixed host port so it survives restart
	destinationBroker, err := NewMQTTContainer(true, "22883")
	require.NoError(t, err, "failed to start destination broker")
	defer destinationBroker.Terminate()

	sourceURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", testUsername, testPassword, sourceBroker.HostIP, sourceBroker.HostPort))
	require.NoError(t, err)
	destinationURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", testUsername, testPassword, destinationBroker.HostIP, destinationBroker.HostPort))
	require.NoError(t, err)

	terminateMirror, err := Mirror(*sourceURL, *destinationURL, []string{}, false, 0, "test", true, nil, nil)
	require.NoError(t, err)
	defer terminateMirror()

	mu := sync.Mutex{}
	var destMessages []paho.Message

	destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-rc")
	token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		destMessages = append(destMessages, msg)
	})
	token.Wait()

	// Phase 1: Baseline
	srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-rc")
	token = srcClient.Publish("test/baseline", byte(0), false, []byte("baseline"))
	token.Wait()

	waitForMessages(t, &mu, &destMessages, 1, 5*time.Second)

	// Phase 2: Stop target broker
	ctx := context.Background()
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer func() { _ = dockerClient.Close() }()

	containerID := destinationBroker.container.GetContainerID()
	stopTimeout := 5
	err = dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTimeout})
	require.NoError(t, err, "failed to stop destination broker")

	time.Sleep(2 * time.Second)

	// Phase 3: Restart target broker
	err = dockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
	require.NoError(t, err, "failed to restart destination broker")

	brokerAddr := fmt.Sprintf("%s:%s", destinationBroker.HostIP, destinationBroker.HostPort)
	waitForTCP(t, brokerAddr, 30*time.Second)

	// Phase 4: Re-subscribe on destination and publish new messages
	destClient.Disconnect(0)
	var destClient2 paho.Client
	for range 15 {
		clientOpts := paho.NewClientOptions().AddBroker(destinationBroker.Uri()).SetAutoReconnect(true).SetMaxReconnectInterval(30 * time.Second).SetUsername(testUsername).SetPassword(testPassword).SetClientID("dest-rc2")
		c := paho.NewClient(clientOpts)
		tok := c.Connect()
		if tok.WaitTimeout(5*time.Second) && tok.Error() == nil && c.IsConnected() {
			destClient2 = c
			break
		}
		time.Sleep(time.Second)
	}
	require.NotNil(t, destClient2, "failed to reconnect subscriber to destination broker")

	mu.Lock()
	destMessages = nil
	mu.Unlock()

	token = destClient2.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		destMessages = append(destMessages, msg)
	})
	token.Wait()

	// Poll: publish on source and wait for message to arrive on destination
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		token = srcClient.Publish("test/after-target-restart", byte(0), false, []byte("recovered"))
		token.Wait()

		time.Sleep(3 * time.Second)

		mu.Lock()
		count := len(destMessages)
		mu.Unlock()
		if count >= 1 {
			break
		}
	}

	mu.Lock()
	require.GreaterOrEqual(t, len(destMessages), 1, "mirroring should resume after target broker restart")
	mu.Unlock()
}

func TestMirror_MessageBurst(t *testing.T) {
	sourceBroker, destinationBroker, _ := setupMirror(t, nil)

	mu := sync.Mutex{}
	var messages []paho.Message
	destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-burst")
	token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, msg)
	})
	token.Wait()

	srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-burst")

	// Publish 100 messages rapidly
	for i := range 100 {
		token = srcClient.Publish("burst/test", byte(0), false, []byte(fmt.Sprintf("msg-%d", i)))
		token.Wait()
	}

	waitForMessages(t, &mu, &messages, 100, 10*time.Second)

	mu.Lock()
	require.Len(t, messages, 100, "all 100 burst messages should be received")
	mu.Unlock()
}

func TestMirror_GracefulShutdown(t *testing.T) {
	sourceBroker, destinationBroker, terminateMirror := setupMirror(t, nil)

	mu := sync.Mutex{}
	var messages []paho.Message
	destClient := NewClient(t, destinationBroker.Uri(), testUsername, testPassword, "dest-shut")
	token := destClient.Subscribe("#", byte(0), func(client paho.Client, msg paho.Message) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, msg)
	})
	token.Wait()

	srcClient := NewClient(t, sourceBroker.Uri(), testUsername, testPassword, "src-shut")

	// Verify messages flow before shutdown
	token = srcClient.Publish("shutdown/before", byte(0), false, []byte("before"))
	token.Wait()
	waitForMessages(t, &mu, &messages, 1, 5*time.Second)

	// Terminate the mirror
	terminateMirror()

	// Record count after shutdown
	mu.Lock()
	countAfterShutdown := len(messages)
	mu.Unlock()

	// Publish more messages — they should NOT be forwarded
	token = srcClient.Publish("shutdown/after", byte(0), false, []byte("after"))
	token.Wait()

	time.Sleep(2 * time.Second)

	mu.Lock()
	require.Equal(t, countAfterShutdown, len(messages), "no new messages should arrive after terminate()")
	mu.Unlock()
}
