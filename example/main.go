package main

import (
	"github.com/fxsml/gopipe"
	azservicebus "github.com/fxsml/gopipe-azservicebus"
	"github.com/fxsml/gopipe/channel"
)

type Article struct {
	ID   int
	Name string
}

func main() {
	client, err := azservicebus.NewClient("your-connection-string")
	if err != nil {
		panic(err)
	}
	publisher := azservicebus.NewPublisher(client, azservicebus.PublisherConfig{})
	defer publisher.Close()

	msg := &gopipe.Message[any]{
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
