package opencodego

import (
	"fmt"
	"net/http"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/internal/useragent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/wire/openai/chatcompletions"
)

const (
	asideTimeout  = 30 * time.Second
	sessionHeader = "X-Opencode-Session"
)

type Client struct {
	*chatcompletions.Client

	UsageURL string
	Token    string

	observer req.Observer
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

	conversation, err := chatcompletions.New(url, requestHeaders(token), model, effort, maxOutputTokens)
	if err != nil {
		return nil, err
	}

	return &Client{Client: conversation, Token: token}, nil
}

func (self *Client) UseSession(id string) {
	self.SetRequestHeader(sessionHeader, id)
}

func (self *Client) ObserveHTTP(observer req.Observer) {
	self.observer = observer
	self.Client.ObserveHTTP(observer)
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

func ToolsSize(tools []tool.Tool) int {
	return chatcompletions.ToolsSize(tools)
}
