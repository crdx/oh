package ollama

import (
	"errors"
	"strings"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/wire/openai/chatcompletions"
)

const (
	EndpointURL  = "http://localhost:11434"
	HostVariable = "OLLAMA_HOST"
)

const (
	completionsPath = "/v1/chat/completions"
	requestTimeout  = 30 * time.Second
)

type Client struct {
	*chatcompletions.Client

	EndpointURL string

	requests *req.Client
}

func New(endpointURL string, model string, effort string, maxOutputTokens int) (*Client, error) {
	endpointURL = strings.TrimRight(endpointURL, "/")
	if endpointURL == "" {
		return nil, errors.New("ollama: EndpointURL is empty")
	}
	if !strings.Contains(endpointURL, "://") {
		endpointURL = "http://" + endpointURL
	}
	turnURL := endpointURL
	if !strings.HasSuffix(turnURL, completionsPath) {
		turnURL += completionsPath
	}

	conversation, err := chatcompletions.New(turnURL, nil, model, effort, maxOutputTokens)
	if err != nil {
		return nil, err
	}

	return &Client{
		Client:      conversation,
		EndpointURL: strings.TrimSuffix(turnURL, completionsPath),
		requests:    req.New(requestTimeout),
	}, nil
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.Client.ObserveHTTP(observer)
	self.requests.Observe(observer)
}

func ToolsSize(tools []tool.Tool) int {
	return chatcompletions.ToolsSize(tools)
}
