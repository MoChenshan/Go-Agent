// Package main implements a simple A2A client example.
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/trpc-go/trpc-a2a-go/trpc"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func main() {
	timeout := flag.Duration("timeout", 60*time.Second, "Request timeout (e.g., 30s, 1m)")
	message := flag.String("message", "write large language model summary", "Message to send to the agent")
	flag.Parse()

	url := "http://localhost:8080"
	target := "ip://localhost:8080"
	trpcHTTPHandler := trpc.NewA2ATRPCHTTPReqHandler(
		"trpc.app.app.agent",
		client.WithTarget(target),
		client.WithTimeout(*timeout),
	)
	a2aClient, err := a2aclient.NewA2AClient(url, a2aclient.WithHTTPReqHandler(trpcHTTPHandler))
	if err != nil {
		log.Fatalf("Failed to create A2A client: %v", err)
	}

	reqMsg := protocol.NewMessage(
		protocol.MessageRoleUser,
		[]protocol.Part{protocol.NewTextPart(*message)},
	)

	response, err := a2aClient.SendMessage(context.Background(), protocol.SendMessageParams{Message: reqMsg})
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	rspMsg := response.Result.(*protocol.Message)
	for _, part := range rspMsg.Parts {
		if textPart, ok := part.(*protocol.TextPart); ok {
			fmt.Printf("Response: %s\n", textPart.Text)
		}
	}
}
