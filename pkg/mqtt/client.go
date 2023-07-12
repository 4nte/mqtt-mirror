package mqtt

import (
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

func NewClient(broker string, username string, password string, isSource bool, clientName string) (paho.Client, error) {
	var role string
	if isSource {
		role = "source"
	} else {
		role = "target"
	}

	if len(clientName) > 10 {
		return nil, fmt.Errorf("client name can have maximum of 10 characters")
	}
	id := fmt.Sprintf("mqtt-mirror-%s", clientName)

	clientOpts := paho.NewClientOptions().AddBroker(broker).SetAutoReconnect(true).SetMaxReconnectInterval(15 * time.Second).SetUsername(username).SetPassword(password).SetClientID(id)

	clientOpts.SetOnConnectHandler(func(client paho.Client) {
		zap.L().Info("connection established", zap.String("broker_uri", broker), zap.String("role", role))
	})
	clientOpts.SetConnectionLostHandler(func(i paho.Client, error error) {
		zap.L().Fatal("connection lost", zap.String("broker_uri", broker), zap.String("role", role))
	})

	client := paho.NewClient(clientOpts)

	token := client.Connect()
	connTimeout := 15 * time.Second
	ok := token.WaitTimeout(connTimeout)
	if !ok {
		err := fmt.Errorf("connection timeout exceeded (%s): %s (%s)", connTimeout.String(), broker, role)
		return nil, err
	}

	return client, nil
}
