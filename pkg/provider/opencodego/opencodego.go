package opencodego

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/internal/useragent"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/wire/anthropic/messages"
	"crdx.org/oh/pkg/wire/openai/chatcompletions"
	"crdx.org/oh/pkg/wire/openai/responses"
)

const (
	asideTimeout  = 30 * time.Second
	sessionHeader = "X-Opencode-Session"

	completionsSuffix = "/chat/completions"
	responsesSuffix   = "/responses"
	messagesSuffix    = "/messages"
)

type conversation interface {
	agent.Provider
	agent.State
	Models(ctx context.Context) ([]agent.Model, error)
	ObserveHTTP(observer req.Observer)
	IdleAfter(after time.Duration)
	SetRequestHeader(name string, value string)
}

type Client struct {
	conversation

	UsageURL string
	Token    string

	responses *responses.Client
	toolsSize func([]tool.Tool) int
	observer  req.Observer
}

func New(
	url string,
	token string,
	model string,
	effort string,
	maxOutputTokens int,
) (*Client, error) {
	for _, setting := range []struct {
		name  string
		value string
	}{
		{"URL", url},
		{"Model", model},
		{"Token", token},
	} {
		if setting.value == "" {
			return nil, fmt.Errorf("chat: %s is empty", setting.name)
		}
	}

	switch wireFor(model) {
	case responsesWire:
		return newResponsesClient(url, token, model, effort)
	case messagesWire:
		return newMessagesClient(url, token, model, effort, maxOutputTokens)
	case completionsWire:
		return newCompletionsClient(url, token, model, effort, maxOutputTokens)
	}

	return nil, fmt.Errorf("chat: no wire format speaks to %s", model)
}

func newCompletionsClient(url string, token string, model string, effort string, maxOutputTokens int) (*Client, error) {
	conversation, err := chatcompletions.New(url, requestHeaders(token), model, effort, maxOutputTokens)
	if err != nil {
		return nil, err
	}

	return &Client{conversation: conversation, Token: token, toolsSize: chatcompletions.ToolsSize}, nil
}

func newResponsesClient(url string, token string, model string, effort string) (*Client, error) {
	conversation, err := responses.NewAt(besideCompletions(url, responsesSuffix), requestHeaders(token), model, effort)
	if err != nil {
		return nil, err
	}

	return &Client{
		conversation: conversation,
		Token:        token,
		responses:    conversation,
		toolsSize:    responses.ToolsSize,
	}, nil
}

func newMessagesClient(url string, token string, model string, effort string, maxOutputTokens int) (*Client, error) {
	conversation, err := messages.NewAt(
		besideCompletions(url, messagesSuffix),
		requestHeaders(token),
		model,
		effort,
		maxOutputTokens,
	)
	if err != nil {
		return nil, err
	}

	return &Client{conversation: conversation, Token: token, toolsSize: messages.ToolsSize}, nil
}

func besideCompletions(completionsAddress string, suffix string) string {
	return strings.TrimSuffix(completionsAddress, completionsSuffix) + suffix
}

func (self *Client) UseSession(id string) {
	self.SetRequestHeader(sessionHeader, id)
	if self.responses != nil {
		self.responses.UseSession(id)
	}
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.conversation.ObserveHTTP(observer)
}

func (self *Client) ToolsSize(tools []tool.Tool) int {
	return self.toolsSize(tools)
}

func (self *Client) observedRequests() *req.Client {
	client := req.New(asideTimeout)
	if self.observer != nil {
		client.Observe(self.observer)
	}

	return client
}

func (self *Client) headers() http.Header {
	return requestHeaders(self.Token)
}

func requestHeaders(token string) http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	header.Set("User-Agent", useragent.Get())
	return header
}
