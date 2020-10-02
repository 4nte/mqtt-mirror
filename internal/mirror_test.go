package internal

import (
	"context"
	"fmt"
	"github.com/docker/go-connections/nat"
	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"
	"net/url"
	"sync"

	"github.com/surgemq/message"
	"github.com/testcontainers/testcontainers-go"
	"path"

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
}

func (b MqttBroker) Uri() string {
	return fmt.Sprintf("tcp://%s:%s", b.HostIP, b.HostPort)
}

func NewMQTTContainer(requireAuth bool) (MqttBroker, error) {
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(b)

	var configFile string
	if requireAuth {
		configFile = "volantmq-config.yaml"
	} else {
		configFile = "volantmq-config-no-auth.yaml"
	}

	configFilePath := path.Join(basepath, configFile)
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "docker.io/volantmq/volantmq:v0.4.0-rc.8",
		ExposedPorts: []string{"1883/tcp"},
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
	}, nil
}

func NewClient(broker string, username string, password string, clientID string) paho.Client {
	clientOpts := paho.NewClientOptions().AddBroker(broker).SetAutoReconnect(true).SetMaxReconnectInterval(15 * time.Second).SetUsername(username).SetPassword(password).SetClientID(clientID)

	clientOpts.SetOnConnectHandler(func(client paho.Client) {
		fmt.Printf("connection established to %s (%s)\n", broker, clientID)
	})
	clientOpts.SetConnectionLostHandler(func(i paho.Client, error error) {
		fmt.Print(fmt.Errorf("connection lost with %s (%s)\n", broker, clientID))
	})

	client := paho.NewClient(clientOpts)

	token := client.Connect()
	connTimeout := 20 * time.Second
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

	terminateMirror := Mirror(*sourceURL, *destinationURL, []string{}, true, 0)

	// Wait for mirror func to startup
	<-time.After(10 * time.Second)
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

	require.Equal(t, len(sourceBrokerMessages), len(destinationBrokerMessages), "Destination broker should have equal amount of messages as the Source broker")
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

func TestMirror_noAuth(t *testing.T) {
	sourceBroker, err := NewMQTTContainer(false)
	if err != nil {
		t.Fatalf("failed to start a source broker: %s", err)
	}
	destinationBroker, err := NewMQTTContainer(false)
	if err != nil {
		t.Fatalf("failed to start a source broker: %s", err)
	}

	// MQTT credentials for both brokers
	username := ""
	password := ""

	// Start Mirror func
	sourceURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", username, password, sourceBroker.HostIP, sourceBroker.HostPort))
	if err != nil {
		panic(err)
	}
	destinationURL, err := url.Parse(fmt.Sprintf("tcp://%s:%s@%s:%s", username, password, destinationBroker.HostIP, destinationBroker.HostPort))
	if err != nil {
		panic(err)
	}

	terminateMirror := Mirror(*sourceURL, *destinationURL, []string{}, true, 0)

	// Wait for mirror func to startup
	<-time.After(10 * time.Second)
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

	require.Equal(t, len(sourceBrokerMessages), len(destinationBrokerMessages), "Destination broker should have equal amount of messages as the Source broker")
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
