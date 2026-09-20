package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func assistantItem(text string) json.RawMessage {
	return encodeItem(message{
		Role:    assistantRole,
		Content: []json.RawMessage{encodeItem(textBlock{Type: "text", Text: text})},
	})
}

func userItem(text string) json.RawMessage {
	return encodeItem(message{
		Role:    userRole,
		Content: []json.RawMessage{encodeItem(textBlock{Type: "text", Text: text})},
	})
}

func TestATurnLeftUnfinishedIsToldToCarryOn(t *testing.T) {
	client := &Client{history: []json.RawMessage{userItem("do it"), assistantItem("I was saying")}}
	client.resumeInterruptedTurn()

	if len(client.history) != 3 {
		t.Fatalf("got %d items, want 3", len(client.history))
	}
	if !strings.Contains(string(client.history[2]), continueInstruction) {
		t.Errorf("got %s", client.history[2])
	}
}

func TestATurnAlreadyToldToCarryOnIsNotToldAgain(t *testing.T) {
	client := &Client{history: []json.RawMessage{userItem("do it"), assistantItem("I was saying")}}
	client.resumeInterruptedTurn()
	client.resumeInterruptedTurn()

	if len(client.history) != 3 {
		t.Errorf("got %d items, want 3", len(client.history))
	}
}

func TestAConversationWaitingOnTheModelIsLeftAlone(t *testing.T) {
	for name, history := range map[string][]json.RawMessage{
		"nothing said yet": nil,
		"a question asked": {userItem("do it")},
		"an answer replied": {
			userItem("do it"), assistantItem("done"), userItem("again"),
		},
		"an empty tail": {
			userItem("do it"), assistantItem("done"), userItem("again"),
			encodeItem(message{Role: assistantRole}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := &Client{history: history}
			client.resumeInterruptedTurn()

			if len(client.history) != len(history) {
				t.Errorf("got %d items, want %d", len(client.history), len(history))
			}
		})
	}
}

func TestWhatWasSentOnceIsSentAgainUnchanged(t *testing.T) {
	client := &Client{instructions: "Be brief."}
	client.AddUserMessage("do it")

	firstRequest := client.requestBody()

	client.history = append(client.history, assistantItem("I was saying"))
	client.resumeInterruptedTurn()
	client.history = append(client.history, assistantItem("carrying on"))
	client.AddUserMessage("thanks")

	secondRequest := client.requestBody()

	if len(secondRequest.Messages) <= len(firstRequest.Messages) {
		t.Fatalf("got %d messages, want more than %d", len(secondRequest.Messages), len(firstRequest.Messages))
	}

	for at, sent := range firstRequest.Messages {
		if string(secondRequest.Messages[at]) != string(sent) {
			t.Errorf("message %d was rewritten:\n%s\n%s", at, sent, secondRequest.Messages[at])
		}
	}

	if string(encodeItem(firstRequest.System)) != string(encodeItem(secondRequest.System)) {
		t.Errorf(
			"the system prompt was rewritten:\n%s\n%s",
			encodeItem(firstRequest.System), encodeItem(secondRequest.System),
		)
	}
}

func TestACorrectionIsSaidInTheConversationRatherThanTheSystemPrompt(t *testing.T) {
	client := &Client{instructions: "Be brief."}
	client.AddUserMessage("do it")
	client.addUserText(invalidToolInputError{toolName: "read"}.getCorrection())

	body := client.requestBody()

	for _, block := range body.System {
		if strings.Contains(block.Text, "could not be used") {
			t.Errorf("the correction reached the system prompt: %s", block.Text)
		}
	}

	lastMessage := body.Messages[len(body.Messages)-1]
	if !strings.Contains(string(lastMessage), "could not be used") {
		t.Errorf("got %s", lastMessage)
	}
}

func TestTheCacheLifetimeIsLeftToTheEndpoint(t *testing.T) {
	asked, err := json.Marshal(ephemeral())
	if err != nil {
		t.Fatal(err)
	}

	if string(asked) != `{"type":"ephemeral"}` {
		t.Errorf("the breakpoint asks for %s, want nothing beyond an ephemeral breakpoint", asked)
	}

	if _, isReported := any(&Client{}).(agent.CacheLifetimeReporter); isReported {
		t.Error("the client declares a cache lifetime it does not ask the endpoint for")
	}
}
