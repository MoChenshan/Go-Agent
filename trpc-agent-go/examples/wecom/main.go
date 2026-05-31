// Package main demonstrates how to bridge a runner to a WeCom AI bot
// websocket.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	twecom "git.woa.com/trpc-go/trpc-agent-go/trpc/server/wecom"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

const (
	appName      = "wecom-agent-demo"
	agentName    = "assistant"
	defaultModel = "deepseek-chat"
)

const (
	envWeComStreamBotID  = "WECOM_STREAM_BOT_ID"
	envWeComStreamSecret = "WECOM_STREAM_SECRET"
	envWeComBotName      = "WECOM_BOT_NAME"
	envWeComStreamWSURL  = "WECOM_STREAM_WS_URL"
)

const (
	agentDescription = "A helpful assistant for Enterprise WeCom."
	agentInstruction = "Answer clearly and keep responses concise."
)

type exampleConfig struct {
	aiBotID      string
	secret       string
	botName      string
	webSocketURL string
}

func main() {
	modelName := flag.String("model", defaultModel, "Model to use")
	enableStream := flag.Bool("stream", true, "Whether to stream replies")
	flag.Parse()

	cfg, err := loadConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	agentInstance := llmagent.New(
		agentName,
		llmagent.WithModel(openai.New(*modelName)),
		llmagent.WithDescription(agentDescription),
		llmagent.WithInstruction(agentInstruction),
	)

	baseRunner := runner.NewRunner(appName, agentInstance)
	defer baseRunner.Close()

	server, err := twecom.New(baseRunner, twecom.Config{
		BotID:        cfg.aiBotID,
		Secret:       cfg.secret,
		BotName:      cfg.botName,
		WebSocketURL: cfg.webSocketURL,
		EnableStream: *enableStream,
	})
	if err != nil {
		log.Fatalf("failed to create wecom server: %v", err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	log.Printf(
		"WeCom AI bot server started for AI bot %q with model %q",
		cfg.aiBotID,
		*modelName,
	)
	if err := server.Run(ctx); err != nil &&
		!errors.Is(err, context.Canceled) {
		log.Fatalf("wecom server stopped: %v", err)
	}
}

func loadConfigFromEnv() (exampleConfig, error) {
	aiBotID, err := requiredEnv(envWeComStreamBotID)
	if err != nil {
		return exampleConfig{}, err
	}

	secret, err := requiredEnv(envWeComStreamSecret)
	if err != nil {
		return exampleConfig{}, err
	}

	return exampleConfig{
		aiBotID:      aiBotID,
		secret:       secret,
		botName:      strings.TrimSpace(os.Getenv(envWeComBotName)),
		webSocketURL: strings.TrimSpace(os.Getenv(envWeComStreamWSURL)),
	}, nil
}

func requiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}
