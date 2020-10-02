package internal

import (
	"fmt"
	"github.com/4nte/mqtt-mirror/pkg/mqtt"
	mqtt2 "github.com/eclipse/paho.mqtt.golang"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func createSourceMessageHandler(targetClient mqtt2.Client, verbose bool) mqtt2.MessageHandler {
	if verbose {
		return func(client mqtt2.Client, message mqtt2.Message) {
			topic := message.Topic()
			payload := message.Payload()
			qos := message.Qos()
			retained := message.Retained()
			fmt.Printf("message replicated (%d bytes): topic=%s, QoS=%b, retained=%s\n", len(payload), topic, qos, strconv.FormatBool(retained))
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

func Mirror(source url.URL, target url.URL, topics []string, verbose bool, timeout time.Duration) func() {
	done := make(chan struct{})
	if timeout > 0 {
		go func() {
			<-time.After(timeout)
			done <- struct{}{}
		}()
	}

	fmt.Printf("mirroring traffic (%s) --> (%s)\n", source.Host, target.Host)

	sourceHost := getBrokerHostString(source)
	sourcePassword, _ := source.User.Password()

	sourceClient, err := mqtt.NewClient(sourceHost, source.User.Username(), sourcePassword, true)
	if err != nil {
		panic(err)
	}

	targetHost := getBrokerHostString(target)
	targetPassword, _ := target.User.Password()
	targetClient, err := mqtt.NewClient(targetHost, target.User.Username(), targetPassword, false)
	if err != nil {
		panic(err)
	}
	qos := byte(0)
	messageHandler := createSourceMessageHandler(targetClient, verbose)
	if len(topics) == 0 {
		// Subscribe to all
		sourceClient.Subscribe("#", qos, messageHandler)
		fmt.Println("mirroring *all* topics")
	} else {
		topicFilterMap := make(map[string]byte)
		for _, topicFilter := range topics {
			topicFilterMap[topicFilter] = qos
		}

		// Subscribe to specified filters
		sourceClient.SubscribeMultiple(topicFilterMap, messageHandler)
		fmt.Printf("mirroring topics: %s\n", strings.Join(topics, ", "))
	}

	terminate := func() {
		sourceClient.Disconnect(0)
		targetClient.Disconnect(0)
	}
	go func() {
		<-done
		terminate()
	}()

	return terminate

}
