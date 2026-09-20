package req

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/internal/transient"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/agent"
)

const bodyLimit = 64 * 1024

type Client struct {
	http     *http.Client
	idle     time.Duration
	observer Observer
}

func New(timeout time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: timeout}}
}

func NewStreaming(responseHeaderTimeout time.Duration, idleTimeout time.Duration) *Client {
	transport, isStandard := http.DefaultTransport.(*http.Transport)
	if !isStandard {
		return &Client{http: &http.Client{}, idle: idleTimeout}
	}

	streaming := transport.Clone()
	streaming.ResponseHeaderTimeout = responseHeaderTimeout

	return &Client{http: &http.Client{Transport: streaming}, idle: idleTimeout}
}

func (self *Client) IdleAfter(after time.Duration) {
	self.idle = after
}

func (self *Client) Observe(observer Observer) {
	self.observer = observer
}

func (self *Client) Stream(
	ctx context.Context, address string, body any, header http.Header,
) (io.ReadCloser, http.Header, error) {
	return self.postJSON(ctx, address, body, header)
}

func (self *Client) JSON(ctx context.Context, address string, body any, target any) error {
	header := http.Header{}
	header.Set("Accept", "application/json")

	responseBody, _, err := self.postJSON(ctx, address, body, header)
	if err != nil {
		return err
	}
	defer func() { _ = responseBody.Close() }()

	if err := json.NewDecoder(responseBody).Decode(target); err != nil {
		return fmt.Errorf("parse the response: %w", err)
	}

	return nil
}

func (self *Client) Get(ctx context.Context, address string, header http.Header, target any) error {
	_, err := self.GetWithHeaders(ctx, address, header, target)

	return err
}

func (self *Client) GetWithHeaders(
	ctx context.Context, address string, header http.Header, target any,
) (http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}

	if header != nil {
		request.Header = header.Clone()
	}

	request.Header.Set("Accept", "application/json")

	body, responseHeader, err := self.do(request, nil)
	if err != nil {
		return responseHeader, err
	}
	defer func() { _ = body.Close() }()

	if err := json.NewDecoder(body).Decode(target); err != nil {
		return responseHeader, fmt.Errorf("parse the response: %w", err)
	}

	return responseHeader, nil
}

func (self *Client) Form(ctx context.Context, address string, form url.Values, target any) error {
	encodedBody := form.Encode()
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, address, strings.NewReader(encodedBody),
	)
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, _, err := self.do(request, []byte(encodedBody))
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := json.NewDecoder(body).Decode(target); err != nil {
		return fmt.Errorf("parse the response: %w", err)
	}

	return nil
}

func (self *Client) postJSON(
	ctx context.Context, address string, body any, header http.Header,
) (io.ReadCloser, http.Header, error) {
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(encodedBody))
	if err != nil {
		return nil, nil, err
	}

	if header != nil {
		request.Header = header.Clone()
	}

	request.Header.Set("Content-Type", "application/json")

	return self.do(request, encodedBody)
}

func (self *Client) do(request *http.Request, requestBody []byte) (io.ReadCloser, http.Header, error) {
	var watchdog *idleWatchdog
	if self.idle > 0 {
		ctx, cancel := context.WithCancel(request.Context())
		watchdog = newIdleWatchdog(self.idle, cancel)
		request = request.WithContext(ctx)
	}

	var exchange ExchangeObserver
	if self.observer != nil {
		exchange = self.observer.Start(Request{
			StartedAt: time.Now(),
			Method:    request.Method,
			URL:       request.URL.String(),
			Protocol:  request.Proto,
			Header:    request.Header.Clone(),
			Body:      bytes.Clone(requestBody),
		})
	}

	response, err := self.http.Do(request)
	if err != nil {
		if watchdog != nil {
			watchdog.stop()
		}

		err = transient.Wrap(err)
		if exchange != nil {
			exchange.Finish(time.Now(), err, false)
		}

		return nil, nil, err
	}

	if watchdog != nil {
		response.Body = watchdog.watch(response.Body)
	}

	if exchange != nil {
		exchange.Response(Response{
			ReceivedAt:   time.Now(),
			Protocol:     response.Proto,
			Status:       response.Status,
			Code:         response.StatusCode,
			Header:       response.Header.Clone(),
			IsCompressed: response.Uncompressed,
		})
		response.Body = &observedBody{
			ReadCloser: response.Body,
			observer:   exchange,
		}
	}

	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()

		return nil, response.Header, refusal(response)
	}

	return response.Body, response.Header, nil
}

type observedBody struct {
	io.ReadCloser

	observer   ExchangeObserver
	isFinished bool
}

func (self *observedBody) Read(buffer []byte) (int, error) {
	count, err := self.ReadCloser.Read(buffer)
	if count > 0 {
		self.observer.Body(time.Now(), bytes.Clone(buffer[:count]))
	}
	if err != nil {
		self.finish(err, false)
	}
	return count, err
}

func (self *observedBody) Close() error {
	err := self.ReadCloser.Close()
	if !self.isFinished {
		self.finish(err, true)
	}
	return err
}

func (self *observedBody) finish(err error, isIncomplete bool) {
	if self.isFinished {
		return
	}
	self.isFinished = true
	if errors.Is(err, io.EOF) {
		err = nil
	}
	self.observer.Finish(time.Now(), err, isIncomplete)
}

var terminalServerStatuses = map[int]bool{
	http.StatusNotImplemented:                true,
	http.StatusHTTPVersionNotSupported:       true,
	http.StatusVariantAlsoNegotiates:         true,
	http.StatusLoopDetected:                  true,
	http.StatusNotExtended:                   true,
	http.StatusNetworkAuthenticationRequired: true,
}

type StatusError struct {
	Status    int
	Code      string
	Message   string
	Body      string
	MediaType string
	Wait      time.Duration
}

func (self *StatusError) Error() string {
	return self.DescribeFailure().Text()
}

func (self *StatusError) DescribeFailure() agent.Failure {
	body := ""
	if self.Message == "" && !isHTMLMediaType(self.MediaType) {
		body = self.Body
	}

	return agent.Failure{
		Kind:       agent.HTTPStatusFailure,
		Message:    self.Message,
		HTTPStatus: self.Status,
		Code:       self.Code,
		Body:       body,
		MediaType:  self.MediaType,
	}
}

func IsRejected(err error) bool {
	var refusedRequest *StatusError

	return errors.As(err, &refusedRequest) && refusedRequest.Status >= 400 && refusedRequest.Status < 500
}

func (self *StatusError) Retriable() bool {
	if self.Status == http.StatusTooManyRequests {
		return true
	}

	return self.Status/100 == 5 && !terminalServerStatuses[self.Status]
}

func (self *StatusError) RetryAfter() time.Duration {
	return self.Wait
}

func refusal(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, bodyLimit))

	refusedRequest := &StatusError{
		Status:    response.StatusCode,
		Body:      string(body),
		MediaType: mediaType(response.Header.Get("Content-Type"), body),
		Wait:      retryAfter(response.Header.Get("Retry-After")),
	}

	var payload struct {
		Error struct {
			Message string          `json:"message"`
			Code    json.RawMessage `json:"code"`
			Type    string          `json:"type"`
		} `json:"error"`

		Detail string `json:"detail"`
	}

	_ = json.Unmarshal(body, &payload)

	refusedRequest.Code = util.JSONScalar(payload.Error.Code)
	if payload.Error.Type != "" &&
		(refusedRequest.Code == "" || refusedRequest.Code == strconv.Itoa(refusedRequest.Status)) {
		refusedRequest.Code = payload.Error.Type
	}

	for _, sentence := range []string{payload.Error.Message, payload.Detail} {
		if sentence != "" {
			refusedRequest.Message = sentence

			break
		}
	}

	return refusedRequest
}

func mediaType(header string, body []byte) string {
	detectedMediaType, _, _ := mime.ParseMediaType(http.DetectContentType(body))
	if isHTMLMediaType(detectedMediaType) {
		return detectedMediaType
	}

	declaredMediaType, _, err := mime.ParseMediaType(header)
	if err == nil {
		return declaredMediaType
	}

	return detectedMediaType
}

func isHTMLMediaType(mediaType string) bool {
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

func CacheLifetime(header http.Header) time.Duration {
	for directive := range strings.SplitSeq(header.Get("Cache-Control"), ",") {
		value, found := strings.CutPrefix(strings.TrimSpace(directive), "max-age=")
		if !found {
			continue
		}

		seconds, err := strconv.Atoi(strings.Trim(value, `"`))
		if err == nil {
			return max(time.Duration(seconds)*time.Second, 0)
		}
	}

	return 0
}

func retryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(header); err == nil {
		return max(time.Duration(seconds)*time.Second, 0)
	}

	if date, err := http.ParseTime(header); err == nil {
		return max(time.Until(date), 0)
	}

	return 0
}
