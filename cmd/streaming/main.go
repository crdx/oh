package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/provider/codex"
	"crdx.org/io/pkg/tool"
)

func main() {
	client, err := codex.Auth("gpt-5.6-sol", "high")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	assistant := agent.New("You are a helpful assistant.", client, []tool.Tool{})
	input := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")

		line, err := input.ReadString('\n')
		if err != nil {
			fmt.Println()
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		answer(assistant, line)
	}
}

func answer(assistant *agent.Agent, message string) {
	isStreamingMessage := false

	for update, err := range assistant.Stream(context.Background(), message, nil) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "\n"+err.Error())
			return
		}

		switch {
		case update.Delta != nil:
			if update.Delta.Kind == agent.ModelMessageEvent {
				isStreamingMessage = true
				fmt.Print(update.Delta.Text)
			}

		case update.Event != nil:
			switch update.Event.Kind {
			case agent.ModelMessageEvent:
				if !isStreamingMessage {
					fmt.Print(update.Event.Text)
				}
				isStreamingMessage = false
			case agent.ToolCallRequestEvent:
				fmt.Printf("\n· %s %s\n", update.Event.Name, update.Event.Arguments)
			case agent.ToolCallResultEvent:
				fmt.Printf("← %s\n", update.Event.Text)
			case agent.StartupEvent, agent.UserMessageEvent, agent.SilentTurnEvent, agent.CacheRebuildEvent, agent.PrefixRewriteEvent,
				agent.ModelReasoningEvent,
				agent.StateChangeEvent, agent.InterruptionEvent, agent.RetryingEvent, agent.FailureEvent:
			}
		}
	}

	fmt.Println()
}
