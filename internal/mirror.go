package internal

import (
	"fmt"
	"github.com/4nte/go-mirror/pkg/mqtt"
	mqtt2 "github.com/eclipse/paho.mqtt.golang"
	"net/url"
)

func createSourceMessageHandler(targetClient mqtt2.Client, verbose bool) mqtt2.MessageHandler {
	if verbose {
		return func(client mqtt2.Client, message mqtt2.Message) {
			topic := message.Topic()
			payload := message.Payload()
			fmt.Printf("topic: %s, %d bytes\n", topic, len(payload))
			targetClient.Publish(message.Topic(), message.Qos(), message.Retained(), message.Payload())
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

func Mirror(source url.URL, target url.URL, topics []string, verbose bool) {
	done := make(chan struct{})

	fmt.Printf("mirroring traffic (%s) --> (%s)\n", source.Hostname(), target.Hostname())

	sourceHost := getBrokerHostString(source)
	sourcePassword, _ := source.User.Password()

	sourceClient := mqtt.NewClient(sourceHost, source.User.String(), sourcePassword, true)

	targetHost := getBrokerHostString(target)
	targetPassword, _ := target.User.Password()
	targetClient := mqtt.NewClient(targetHost, target.User.String(), targetPassword, false)

	qos := byte(0)
	messageHandler := createSourceMessageHandler(targetClient, verbose)
	if len(topics) == 0 {
		// Subscribe to all
		sourceClient.Subscribe("#", qos, messageHandler)
	} else {
		topicFilterMap := make(map[string]byte)
		for _, topicFilter := range topics {
			topicFilterMap[topicFilter] = qos
		}

		// Subscribe to specified filters
		sourceClient.SubscribeMultiple(topicFilterMap, messageHandler)
	}

	<-done
}
