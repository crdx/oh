package req_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
)

type observedExchange struct {
	request    req.Request
	response   req.Response
	body       []byte
	isFinished bool
	incomplete bool
	err        error
}

type observer struct{ exchange *observedExchange }

func (self observer) Start(request req.Request) req.ExchangeObserver {
	self.exchange.request = request
	return self.exchange
}

func (self *observedExchange) Response(response req.Response) { self.response = response }
func (self *observedExchange) Body(_ time.Time, body []byte)  { self.body = append(self.body, body...) }

func (self *observedExchange) Finish(_ time.Time, err error, isIncomplete bool) {
	self.isFinished, self.err, self.incomplete = true, err, isIncomplete
}

func TestObserverSeesTheBytesConsumedByTheCaller(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("response body"))
	}))
	t.Cleanup(server.Close)

	exchange := &observedExchange{}
	client := req.New(time.Second)
	client.Observe(observer{exchange: exchange})
	body, _, err := client.Stream(t.Context(), server.URL, map[string]string{"secret": "no"}, http.Header{"X-Test": {"value"}})
	if err != nil {
		t.Fatal(err)
	}
	consumed := make([]byte, 8)
	if _, err := io.ReadFull(body, consumed); err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}

	if string(exchange.body) != "response" {
		t.Errorf("got consumed body %q", exchange.body)
	}
	if !exchange.isFinished || !exchange.incomplete || exchange.err != nil {
		t.Errorf("expected an incomplete close, got %+v", exchange)
	}
	if exchange.request.Method != http.MethodPost || exchange.request.Header.Get("X-Test") != "value" {
		t.Errorf("unexpected request snapshot: %+v", exchange.request)
	}
	if exchange.response.Code != http.StatusOK {
		t.Errorf("unexpected response snapshot: %+v", exchange.response)
	}
}
