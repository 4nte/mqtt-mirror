package mqtt

import (
	"fmt"
	paho "github.com/eclipse/paho.mqtt.golang"
	"time"
)

func NewClient(broker string, username string, password string, isSource bool) paho.Client {
	var alias string
	if isSource {
		alias = "source"
	} else {
		alias = "target"
	}
	id := fmt.Sprintf("mqtt-mirror-%s", alias)

	clientOpts := paho.NewClientOptions().AddBroker(broker).SetAutoReconnect(true).SetMaxReconnectInterval(3 * time.Minute).SetUsername(username).SetPassword(password).SetClientID(id)

	clientOpts.SetOnConnectHandler(func(client paho.Client) {
		fmt.Printf("connection established to %s (%s)\n", broker, alias)
		// TODO: channel
	})
	clientOpts.SetConnectionLostHandler(func(i paho.Client, error error) {
		fmt.Print(fmt.Errorf("connection lost with %s (%s)", broker, alias))
		// TODO: channel
	})

	client := paho.NewClient(clientOpts)

	token := client.Connect()
	connTimeout := 15 * time.Second
	ok := token.WaitTimeout(connTimeout)
	if !ok {
		err := fmt.Errorf("connection timeout exceeded (%s): %s (%s)", connTimeout.String(), broker, alias)
		panic(err)
	}

	return client
}
