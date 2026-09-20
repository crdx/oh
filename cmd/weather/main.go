package main

import (
	"context"
	"fmt"
	"os"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/provider/codex"
	"crdx.org/io/pkg/tool"
)

func main() {
	type WeatherParams struct {
		City string
	}

	weather := tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args WeatherParams) (string, string) { return args.City, "" },
	).Plain(func(_ context.Context, _ WeatherParams) (string, error) {
		return "Cloudy with a chance of meatballs.", nil
	})

	client, err := codex.Auth("gpt-5.6-sol", "high")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	assistant := agent.New(
		"You are a helpful weatherperson",
		client,
		[]tool.Tool{weather},
	)

	answer, _ := assistant.Send(context.Background(), "what is the weather in London?")
	fmt.Println(answer)
}
