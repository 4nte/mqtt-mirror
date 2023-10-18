package internal

import (
	"net/url"
	"time"

	mqtt2 "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"

	"github.com/4nte/mqtt-mirror/pkg/mqtt"
)

func createSourceMessageHandler(targetClient mqtt2.Client, verbose bool) mqtt2.MessageHandler {
	if verbose {
		return func(client mqtt2.Client, message mqtt2.Message) {
			topic := message.Topic()
			payload := message.Payload()
			qos := message.Qos()
			retained := message.Retained()
			zap.L().
				Info("message replicated", zap.Int("bytes_len", len(payload)), zap.String("topic", topic), zap.Int("QoS", int(qos)), zap.Bool("retained", retained))
			targetClient.Publish(
				message.Topic(),
				message.Qos(),
				message.Retained(),
				message.Payload(),
			)
		}
	}

	return func(client mqtt2.Client, message mqtt2.Message) {
		targetClient.Publish(message.Topic(), message.Qos(), message.Retained(), message.Payload())
	}
}

type Broker struct {
	Scheme   string
	Host     string
	Port     string
	Username string
	Password string
}

func getBrokerHostString(broker url.URL) string {
	host := ""
	if broker.Scheme != "" {
		host = broker.Scheme + "://"
	} else {
		// Default scheme
		host = "tcp://"
	}

	host += broker.Host
	return host
}

func Mirror(
	source url.URL,
	target url.URL,
	topics []string,
	verbose bool,
	timeout time.Duration,
	instanceName string,
) (func(), error) {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)
	defer logger.Sync() // flushes buf

	done := make(chan struct{})
	if timeout > 0 {
		go func() {
			<-time.After(timeout)
			done <- struct{}{}
		}()
	}
	zap.L().Sugar().Infof("using clientName: %s", instanceName)

	zap.L().
		Info("mirroring traffic", zap.String("source_host", source.Host), zap.String("target_host", target.Host))

	sourceHost := getBrokerHostString(source)
	sourcePassword, _ := source.User.Password()

	targetHost := getBrokerHostString(target)
	targetPassword, _ := target.User.Password()
	targetClient, err := mqtt.NewClient(
		targetHost,
		target.User.Username(),
		targetPassword,
		false,
		instanceName,
		func(c mqtt2.Client) {},
	)
	if err != nil {
		return func() {}, err
	}

	qos := byte(0)
	messageHandler := createSourceMessageHandler(targetClient, verbose)
	onConnHandler := func(client mqtt2.Client) {
		if len(topics) == 0 {
			// Subscribe to all
			client.Subscribe("#", qos, messageHandler)
			zap.L().Info("mirroring *all* topics")
		} else {
			topicFilterMap := make(map[string]byte)
			for _, topicFilter := range topics {
				topicFilterMap[topicFilter] = qos
			}

			// Subscribe to specified filters
			client.SubscribeMultiple(topicFilterMap, messageHandler)
			zap.L().Info("mirroring messages", zap.Strings("topics", topics))
		}
	}

	sourceClient, err := mqtt.NewClient(
		sourceHost,
		source.User.Username(),
		sourcePassword,
		true,
		instanceName,
		onConnHandler,
	)
	if err != nil {
		return func() {}, err
	}

	terminate := func() {
		sourceClient.Disconnect(0)
		targetClient.Disconnect(0)
	}
	go func() {
		<-done
		terminate()
	}()

	return terminate, nil
}
