package internal

import (
	"fmt"
	"net/url"
	"time"

	mqtt2 "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"

	"github.com/4nte/mqtt-mirror/pkg/mqtt"
)

func createSourceMessageHandler(targetClient mqtt2.Client, verbose bool, metrics *Metrics) mqtt2.MessageHandler {
	return func(client mqtt2.Client, message mqtt2.Message) {
		topic := message.Topic()
		payload := message.Payload()
		qos := message.Qos()
		retained := message.Retained()
		qosStr := fmt.Sprintf("%d", qos)

		if metrics != nil {
			metrics.MessagesReceived.WithLabelValues(qosStr).Inc()
			metrics.MessageSize.Observe(float64(len(payload)))
		}

		if verbose {
			zap.L().Info("message replicated",
				zap.Int("bytes_len", len(payload)),
				zap.String("topic", topic),
				zap.Int("QoS", int(qos)),
				zap.Bool("retained", retained))
		}

		start := time.Now()
		token := targetClient.Publish(topic, qos, retained, payload)
		ok := token.WaitTimeout(10 * time.Second)
		elapsed := time.Since(start).Seconds()

		if metrics != nil {
			metrics.PublishDuration.Observe(elapsed)
		}

		if !ok || token.Error() != nil {
			if metrics != nil {
				metrics.PublishErrors.Inc()
			}
			if token.Error() != nil {
				zap.L().Error("publish failed", zap.String("topic", topic), zap.Error(token.Error()))
			} else {
				zap.L().Error("publish timed out", zap.String("topic", topic))
			}
		} else if metrics != nil {
			metrics.MessagesPublished.WithLabelValues(qosStr).Inc()
		}
	}
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
	cleanSession bool,
	health *HealthServer,
	metrics *Metrics,
) (func(), error) {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)
	defer func() { _ = logger.Sync() }() // flushes buf

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
		true,
		func(c mqtt2.Client) {
			if metrics != nil {
				metrics.TargetConnected.Set(1)
			}
		},
		func(c mqtt2.Client, err error) {
			if metrics != nil {
				metrics.TargetConnected.Set(0)
			}
		},
	)
	if err != nil {
		return func() {}, err
	}

	qos := byte(0)
	messageHandler := createSourceMessageHandler(targetClient, verbose, metrics)
	onConnHandler := func(client mqtt2.Client) {
		if len(topics) == 0 {
			// Subscribe to all
			token := client.Subscribe("#", qos, messageHandler)
			token.Wait()
			if token.Error() != nil {
				zap.L().Error("subscribe failed", zap.Error(token.Error()))
			} else {
				zap.L().Info("mirroring *all* topics")
			}
		} else {
			topicFilterMap := make(map[string]byte)
			for _, topicFilter := range topics {
				topicFilterMap[topicFilter] = qos
			}

			// Subscribe to specified filters
			token := client.SubscribeMultiple(topicFilterMap, messageHandler)
			token.Wait()
			if token.Error() != nil {
				zap.L().Error("subscribe failed", zap.Error(token.Error()))
			} else {
				zap.L().Info("mirroring messages", zap.Strings("topics", topics))
			}
		}
	}

	sourceClient, err := mqtt.NewClient(
		sourceHost,
		source.User.Username(),
		sourcePassword,
		true,
		instanceName,
		cleanSession,
		func(c mqtt2.Client) {
			if metrics != nil {
				metrics.SourceConnected.Set(1)
			}
			onConnHandler(c)
		},
		func(c mqtt2.Client, err error) {
			if metrics != nil {
				metrics.SourceConnected.Set(0)
			}
		},
	)
	if err != nil {
		return func() {}, err
	}

	if health != nil {
		health.SetClients(sourceClient, targetClient)
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
