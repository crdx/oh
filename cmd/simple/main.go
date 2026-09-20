package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/tool"
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
	for update, err := range assistant.Stream(context.Background(), message, nil) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "\n"+err.Error())
			return
		}
		if update.Event == nil {
			continue
		}

		switch update.Event.Kind {
		case agent.ModelMessageEvent:
			fmt.Print(update.Event.Text)
		case agent.ToolCallRequestEvent:
			fmt.Printf("\n· %s %s\n", update.Event.Name, update.Event.Arguments)
		case agent.ToolCallResultEvent:
			fmt.Printf("← %s\n", update.Event.Text)
		case agent.StartupEvent, agent.UserMessageEvent, agent.SilentTurnEvent, agent.CacheRebuildEvent, agent.PrefixRewriteEvent,
			agent.ModelReasoningEvent,
			agent.StateChangeEvent, agent.InterruptionEvent, agent.RetryingEvent, agent.FailureEvent:
		}
	}

	fmt.Println()
}
