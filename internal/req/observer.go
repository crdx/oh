package req

import (
	"net/http"
	"time"
)

type Request struct {
	StartedAt time.Time
	Method    string
	URL       string
	Protocol  string
	Header    http.Header
	Body      []byte
}

type Response struct {
	ReceivedAt   time.Time
	Protocol     string
	Status       string
	Code         int
	Header       http.Header
	IsCompressed bool
}

type Observer interface {
	Start(request Request) ExchangeObserver
}

type CompressionObserver interface {
	CompressionRejected()
}

type ExchangeObserver interface {
	Response(response Response)
	Body(readAt time.Time, body []byte)
	Finish(finishedAt time.Time, err error, isIncomplete bool)
}
