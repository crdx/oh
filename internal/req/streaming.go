package req

import (
	"net/http"
	"time"
)

const DefaultStreamTimeout = 10 * time.Minute

type StreamTimeouts struct {
	Response time.Duration
	Idle     time.Duration
}

func newStreamingHTTP(responseTimeout time.Duration) *http.Client {
	transport, isStandard := http.DefaultTransport.(*http.Transport)
	if !isStandard {
		return &http.Client{}
	}
	streaming := transport.Clone()
	streaming.ResponseHeaderTimeout = responseTimeout
	streaming.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: healthCheckAfter,
		PingTimeout:     healthCheckAnswerWithin,
	}
	return &http.Client{Transport: streaming}
}

func (self *Client) TakeStreamTimeoutsFrom(source func() StreamTimeouts) {
	self.streamTimeouts = source
}

func (self *Client) streamSettings() (*http.Client, time.Duration) {
	if self.streamTimeouts == nil {
		return self.http, self.idle
	}
	timeouts := self.streamTimeouts()
	if timeouts.Response <= 0 {
		timeouts.Response = DefaultStreamTimeout
	}
	if timeouts.Idle <= 0 {
		timeouts.Idle = DefaultStreamTimeout
	}
	self.streamMutex.Lock()
	defer self.streamMutex.Unlock()
	if self.streamHTTP == nil || self.streamResponseTimeout != timeouts.Response {
		previous := self.streamHTTP
		self.streamHTTP = newStreamingHTTP(timeouts.Response)
		self.streamResponseTimeout = timeouts.Response
		if previous != nil {
			previous.CloseIdleConnections()
		}
	}
	return self.streamHTTP, timeouts.Idle
}
