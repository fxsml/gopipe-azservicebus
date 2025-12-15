package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	azservicebus "github.com/fxsml/gopipe-azservicebus"
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
	defer client.Close(context.Background())

	// Create sender
	sender := azservicebus.NewSender(client, azservicebus.SenderConfig{})
	defer sender.Close()

	// Create message payload
	article := Article{
		ID:   1,
		Name: "Example",
	}

	// Marshal to JSON
	body, err := json.Marshal(article)
	if err != nil {
		panic(err)
	}

	// Create message
	msg := message.New(body, nil)

	// Send message
	err = sender.Send(context.Background(), "your-topic-name", []*message.Message{msg})
	if err != nil {
		panic(err)
	}

	fmt.Println("Message sent successfully!")
}
