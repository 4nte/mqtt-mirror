package internal

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"path"

	"github.com/surgemq/message"
	"github.com/testcontainers/testcontainers-go"

	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

//func NewMQTTServer(port int) *service.Server {
//	// Create a new server
//	srv := &service.Server{
//		KeepAlive:        300,               // seconds
//		ConnectTimeout:   2,                 // seconds
//		SessionsProvider: "mem",             // keeps sessions in memory
//		Authenticator:    "mockSuccess",     // always succeed
//		TopicsProvider:   "mem",             // keeps topic subscriptions in memory
//	}
//
//
//	uri := fmt.Sprintf("tcp://localhost:%d", port)
//	go func() {
//		if err := srv.ListenAndServe(uri); err != nil {
//			panic(err)
//		}
//	}()
//
//
//	return srv
//}

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
		//WaitingFor:   wait.ForHTTP("/health/ready").WithPort("8080"),
		//WaitingFor:   wait.ForListeningPort("1883/tcp"),
		WaitingFor: wait.ForLog("listener state: id: :1883 status: started"),
		Env: map[string]string{
			"VOLANTMQ_CONFIG": "/etc/volantmq-config.yaml",
		},
		//VolumeMounts: map[string]string{
		//	"configVolume": "/home/anteg/git/mqtt-mirror/internal",
		//},
		BindMounts: map[string]string{
			configFilePath: "/etc/volantmq-config.yaml",
		},
	}
	brokerContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
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

	err = brokerContainer.Start(context.Background())
	if err != nil {
		return MqttBroker{}, nil
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

func NewClient(broker string, username string, password string, clientID string) paho.Client {
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
	if !ok {
		err := fmt.Errorf("connection timeout exceeded (%s): %s (%s)", connTimeout.String(), broker, clientID)
		panic(err)
	}

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
	if err != nil {
		panic(err)
	}
	destinationURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", username, password, destinationBroker.HostIP, destinationBroker.HostPort))
	if err != nil {
		panic(err)
	}

	terminateMirror, err := Mirror(*sourceURL, *destinationURL, []string{}, true, 0, "")
	require.NoError(t, err)

	mutex := sync.Mutex{}

	defer sourceBroker.Terminate()
	defer destinationBroker.Terminate()

	var sourceBrokerMessages []paho.Message
	var destinationBrokerMessages []paho.Message

	fmt.Println(sourceBroker.Uri())
	// Create client and subscribe to all topics
	sourceBrokerClient := NewClient(sourceBroker.Uri(), username, password, "source-client")
	token := sourceBrokerClient.Subscribe("#", message.QosAtLeastOnce, func(client paho.Client, msg paho.Message) {
		mutex.Lock()
		defer mutex.Unlock()
		sourceBrokerMessages = append(sourceBrokerMessages, msg)
	})
	token.Wait()

	// Create client and subscribe to all topics
	destinationBrokerClient := NewClient(destinationBroker.Uri(), username, password, "destination-client")
	token = destinationBrokerClient.Subscribe("#", message.QosAtLeastOnce, func(client paho.Client, msg paho.Message) {
		mutex.Lock()
		defer mutex.Unlock()
		destinationBrokerMessages = append(destinationBrokerMessages, msg)
	})
	token.Wait()

	token = sourceBrokerClient.Publish("test/msg1", message.QosAtLeastOnce, false, []byte("foo"))
	token.Wait()

	token = sourceBrokerClient.Publish("test/msg2", message.QosAtLeastOnce, false, []byte("foo"))
	token.Wait()

	token = sourceBrokerClient.Publish("test/msg3", message.QosAtLeastOnce, false, []byte("foo"))
	token.Wait()

	<-time.After(1 * time.Second)
	terminateMirror()

	require.Lenf(t, sourceBrokerMessages, 3, "Source broker should have 3 messages")
	require.Lenf(t, destinationBrokerMessages, 3, "destination broker should have 3 messages")
	for _, sourceMessage := range sourceBrokerMessages {
		var isDuplicated bool
		for _, msg := range destinationBrokerMessages {
			if string(sourceMessage.Payload()) == string(msg.Payload()) {
				isDuplicated = true
				break
			}
		}
		require.True(t, isDuplicated, "message not duplicated")
	}
}

func waitForTCP(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
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

	terminateMirror, err := Mirror(*sourceURL, *destinationURL, []string{}, true, 0, "")
	require.NoError(t, err)
	defer terminateMirror()

	mutex := sync.Mutex{}
	var destinationMessages []paho.Message

	// Subscribe on destination to verify mirrored messages
	destClient := NewClient(destinationBroker.Uri(), username, password, "dest-sub")
	token := destClient.Subscribe("#", message.QosAtLeastOnce, func(client paho.Client, msg paho.Message) {
		mutex.Lock()
		defer mutex.Unlock()
		destinationMessages = append(destinationMessages, msg)
	})
	token.Wait()

	// Phase 1: Publish before restart — verify baseline mirroring works
	srcClient := NewClient(sourceBroker.Uri(), username, password, "src-pub")
	token = srcClient.Publish("test/before", message.QosAtLeastOnce, false, []byte("before-restart"))
	token.Wait()

	time.Sleep(2 * time.Second)
	mutex.Lock()
	require.Len(t, destinationMessages, 1, "baseline: message should be mirrored before restart")
	mutex.Unlock()

	// Phase 2: Restart the source broker to simulate disconnection
	ctx := context.Background()
	dockerClient, err := client.NewEnvClient()
	require.NoError(t, err, "failed to create docker client")
	defer dockerClient.Close()

	containerID := sourceBroker.container.GetContainerID()
	stopTimeout := 5 * time.Second
	err = dockerClient.ContainerStop(ctx, containerID, &stopTimeout)
	require.NoError(t, err, "failed to stop source broker")

	time.Sleep(2 * time.Second)

	err = dockerClient.ContainerStart(ctx, containerID, types.ContainerStartOptions{})
	require.NoError(t, err, "failed to restart source broker")

	// Wait for the MQTT broker inside the container to accept TCP connections
	brokerAddr := fmt.Sprintf("%s:%s", sourceBroker.HostIP, sourceBroker.HostPort)
	waitForTCP(t, brokerAddr, 30*time.Second)

	// Phase 3: Publish after restart — verify mirroring resumed.
	// Disconnect old test publisher and wait for the broker to accept MQTT
	// connections (TCP being open is not enough — the MQTT handler needs time).
	srcClient.Disconnect(0)
	var srcClient2 paho.Client
	for i := 0; i < 15; i++ {
		func() {
			defer func() { recover() }()
			c := NewClient(sourceBroker.Uri(), username, password, "src-pub2")
			if c.IsConnected() {
				srcClient2 = c
			}
		}()
		if srcClient2 != nil {
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
		token = srcClient2.Publish("test/after", message.QosAtLeastOnce, false, []byte("after-restart"))
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

// TODO: enable after volantmq broker supports anonymous logins
//func TestMirror_noAuth(t *testing.T) {
//	sourceBroker, err := NewMQTTContainer(false)
//	if err != nil {
//		t.Fatalf("failed to start a source broker: %s", err)
//	}
//	destinationBroker, err := NewMQTTContainer(false)
//	if err != nil {
//		t.Fatalf("failed to start a source broker: %s", err)
//	}
//
//
//	// Start Mirror func
//	sourceURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s",sourceBroker.HostIP, sourceBroker.HostPort))
//	if err != nil {
//		panic(err)
//	}
//	destinationURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s", destinationBroker.HostIP, destinationBroker.HostPort))
//	if err != nil {
//		panic(err)
//	}
//	fmt.Println(sourceBroker.HostPort, destinationBroker.HostPort)
//	//<-time.After(30 * time.Second)
//
//	terminateMirror, err := Mirror(*sourceURL, *destinationURL, []string{}, true, 0)
//	require.NoError(t, err)
//	// Wait for mirror func to startup
//	<-time.After(10 * time.Second)
//	mutex := sync.Mutex{}
//
//	defer sourceBroker.Terminate()
//	defer destinationBroker.Terminate()
//
//	var sourceBrokerMessages []paho.Message
//	var destinationBrokerMessages []paho.Message
//
//	fmt.Println(sourceBroker.Uri())
//	// Create client and subscribe to all topics
//	sourceBrokerClient := NewClient(sourceBroker.Uri(), "", "", "source-client")
//	token := sourceBrokerClient.Subscribe("#", message.QosAtLeastOnce, func(client paho.Client, msg paho.Message) {
//		mutex.Lock()
//		defer mutex.Unlock()
//		sourceBrokerMessages = append(sourceBrokerMessages, msg)
//	})
//	token.Wait()
//
//	// Create client and subscribe to all topics
//	destinationBrokerClient := NewClient(destinationBroker.Uri(), "", "", "destination-client")
//	token = destinationBrokerClient.Subscribe("#", message.QosAtLeastOnce, func(client paho.Client, msg paho.Message) {
//		mutex.Lock()
//		defer mutex.Unlock()
//		destinationBrokerMessages = append(destinationBrokerMessages, msg)
//	})
//	token.Wait()
//
//	token = sourceBrokerClient.Publish("test/msg1", message.QosAtLeastOnce, false, []byte("foo"))
//	token.Wait()
//
//	token = sourceBrokerClient.Publish("test/msg2", message.QosAtLeastOnce, false, []byte("bar"))
//	token.Wait()
//
//	token = sourceBrokerClient.Publish("test/msg3", message.QosAtLeastOnce, false, []byte("baz"))
//	token.Wait()
//
//	<-time.After(1 * time.Second)
//	terminateMirror()
//
//	require.Lenf(t, sourceBrokerMessages, 3, "Source broker should have 3 messages")
//	require.Lenf(t, destinationBrokerMessages, 3, "destination broker should have 3 messages")
//	for _, sourceMessage := range sourceBrokerMessages {
//		var isDuplicated bool
//		for _, msg := range destinationBrokerMessages {
//			if string(sourceMessage.Payload()) == string(msg.Payload()) {
//				isDuplicated = true
//				break
//			}
//		}
//		require.True(t, isDuplicated, "message not duplicated")
//	}
//}
