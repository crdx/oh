package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/internal/sse"
	"crdx.org/oh/internal/useragent"
)

const (
	searchHeaderTimeout = 2 * time.Minute
	searchIdleTimeout   = 2 * time.Minute
	searchInstructions  = "Search the web for current, reliable information. Answer directly and cite sources with Markdown links."
)

const SearchEffort = "high"

type SearchClient struct {
	URL   string
	Model string

	tokens   TokenSource
	session  string
	requests *req.Client
}

func NewSearch(tokens TokenSource, model string) (*SearchClient, error) {
	client := &SearchClient{
		URL:      Endpoint,
		Model:    model,
		tokens:   tokens,
		session:  newToken(),
		requests: req.NewStreaming(searchHeaderTimeout, searchIdleTimeout),
	}

	if client.Model == "" {
		return nil, errors.New("codex search: Model is empty")
	}

	return client, nil
}

func (self *SearchClient) IdleAfter(after time.Duration) {
	self.requests.IdleAfter(after)
}

func (self *SearchClient) ObserveHTTP(observer req.Observer) {
	self.requests.Observe(observer)
	if source, ok := self.tokens.(observedTokenSource); ok {
		source.ObserveHTTP(observer)
	}
}

func (self *SearchClient) Search(ctx context.Context, query string) (string, error) {
	token, err := self.tokens.Token()
	if err != nil {
		return "", err
	}

	stream, _, err := self.requests.Stream(ctx, self.URL, searchRequest{
		Model:        self.Model,
		Instructions: searchInstructions,
		Input:        []userMessage{{Role: "user", Content: query}},
		Store:        false,
		Stream:       true,
		Reasoning:    reasoning{Effort: SearchEffort, Summary: Summary},
		Tools: []searchTool{{
			Type:              "web_search",
			SearchContextSize: "high",
		}},
	}, requestHeaders(token, self.session))
	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}
	defer func() { _ = stream.Close() }()

	return readSearchReply(stream)
}

type searchRequest struct {
	Model        string        `json:"model"`
	Instructions string        `json:"instructions"`
	Input        []userMessage `json:"input"`
	Store        bool          `json:"store"`
	Stream       bool          `json:"stream"`
	Reasoning    reasoning     `json:"reasoning"`
	Tools        []searchTool  `json:"tools"`
}

type searchTool struct {
	Type              string `json:"type"`
	SearchContextSize string `json:"search_context_size"`
}

type searchEvent struct {
	Type     string            `json:"type"`
	Delta    string            `json:"delta"`
	Error    *searchEventError `json:"error"`
	Response *struct {
		Error *searchEventError `json:"error"`
	} `json:"response"`
}

type searchEventError struct {
	Message string `json:"message"`
}

func readSearchReply(reader io.Reader) (string, error) {
	var output strings.Builder

	err := sse.Read(reader, func(payload string) (bool, error) {
		if payload == "[DONE]" {
			return true, nil
		}

		var event searchEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return false, fmt.Errorf("could not parse the web search response: %w", err)
		}

		if event.Type == "response.output_text.delta" {
			_, _ = output.WriteString(event.Delta)
		}
		if event.Error != nil && event.Error.Message != "" {
			return false, errors.New(event.Error.Message)
		}
		if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
			return false, errors.New(event.Response.Error.Message)
		}

		return event.Type == "response.completed", nil
	})
	if err != nil {
		return "", fmt.Errorf("could not read the web search response: %w", err)
	}
	if output.Len() == 0 {
		return "", errors.New("web search returned no answer")
	}

	return output.String(), nil
}

func requestHeaders(token Token, session string) http.Header {
	header := http.Header{}

	header.Set("Authorization", "Bearer "+token.Access)
	header.Set("Chatgpt-Account-Id", token.AccountID)
	header.Set("Originator", Originator)
	header.Set("Openai-Beta", "responses=experimental")
	header.Set("Accept", "text/event-stream")
	header.Set("Session_id", session)
	header.Set("User-Agent", useragent.Get())

	return header
}
