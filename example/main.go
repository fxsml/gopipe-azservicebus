package main

import (
	"os"

	azservicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/channel"
	"github.com/fxsml/gopipe/message"
)

type Article struct {
	ID   int
	Name string
}

func main() {
	connectionString := os.Getenv("AZURE_SERVICEBUS_CONNECTION_STRING")

	client, err := azservicebus.NewClient(connectionString)
	if err != nil {
		panic(err)
	}
	publisher := azservicebus.NewPublisher(client, azservicebus.PublisherConfig{})
	defer publisher.Close()

	msg := &message.Message{
		Payload: Article{
			ID:   1,
			Name: "Example",
		},
	}

	done, err := publisher.Publish("your-topic-name", channel.FromValues(msg))
	if err != nil {
		panic(err)
	}

	// Wait for publishing to complete
	<-done
}
