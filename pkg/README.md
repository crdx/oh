## Authentication

Implementors glue the auth flow together themselves. The **oh** implementation handles it with `-L`:

```bash
go run . -L codex
go run . -L opencode-go
go run . -L anthropic
```

Omit the provider to choose interactively.

## Usage

```go
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
    return "Take a guess.", nil
})

client, _ := codex.Auth("gpt-5.6-sol", "high")

assistant := agent.New("You are a helpful weatherperson", client, []tool.Tool{weather})

answer, _ := assistant.Send(ctx, "what is the weather in London?")
fmt.Println(answer) // => "It's probably raining."
```

`Send` blocks until the turn is over. Cancelling the context ends the request the turn is waiting on.

`Stream` is the same turn as transient prose deltas and completed events. Deltas are suitable for live rendering; only events belong in durable history.

```go
for update, err := range assistant.Stream(ctx, "what is the weather in London?", nil) {
    if err != nil {
        return err
    }

    switch {
    case update.Delta != nil:
        if update.Delta.Kind == agent.ModelMessageEvent {
            fmt.Print(update.Delta.Text)
        }
    case update.Event != nil && update.Event.Kind == agent.ToolCallRequestEvent:
        fmt.Println("call", update.Event.Name, update.Event.Arguments)
    case update.Event != nil && update.Event.Kind == agent.ToolCallResultEvent:
        fmt.Println("result", update.Event.Text)
    }
}
```

`Send` collects the completed `ModelMessageEvent` blocks and returns their text.

## Implementations

### simulate

Stand in for every provider endpoint at once, playing scenarios defined in TOML files.

```bash
go run ./cmd/simulate --scenario internal/sim/scenarios/success.toml
```

The simulator deals in wire formats, not providers. A provider speaks one of them, and more than one provider can speak the same one.

| Wire format      | Served at              | Spoken by               |
|------------------|------------------------|-------------------------|
| Responses        | `/v1/codex/responses`  | `codex`                 |
| Chat Completions | `/v1/chat/completions` | `opencode-go`, `ollama` |
| Messages         | `/v1/messages`         | `anthropic`             |

## Examples

| Command         | Shows                                             |
|-----------------|---------------------------------------------------|
| `cmd/weather`   | A tool that answers questions about the weather   |
| `cmd/streaming` | A prompt loop printing each event as it arrives   |
| `cmd/simple`    | The same loop, with text fragments glued together |
