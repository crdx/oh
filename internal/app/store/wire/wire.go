package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"

	"crdx.org/oh/internal/req"
)

const redacted = "[REDACTED]"

var bearerPattern = regexp.MustCompile(`(?i)bearer[ \t]+[^\s"']+`)

type Meta struct {
	Name, Model, Effort, Provider, Workspace string
	StartedAt                                time.Time
}

type Recorder struct {
	mutex     sync.Mutex
	file      *os.File
	encoder   *zstd.Encoder
	hasFailed bool
	next      int
	report    func(error)
}

func Open(path string, meta Meta, report func(error)) (*Recorder, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600) //nolint:gosec // the parent store supplies the fixed bundle path
	if err != nil {
		return nil, err
	}

	next, isNew, err := resume(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	encoder, err := zstd.NewWriter(
		file,
		zstd.WithWindowSize(windowBytes),
		zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
		zstd.WithEncoderConcurrency(1),
		zstd.WithEncoderCRC(false),
	)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	if isNew {
		if _, err := io.WriteString(encoder, transcriptHeader(meta)); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := encoder.Flush(); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return &Recorder{file: file, encoder: encoder, next: next, report: report}, nil
}

func transcriptHeader(meta Meta) string {
	return fmt.Sprintf("# HTTP transcript\n# session: %s\n# started: %s\n# model: %s\n# effort: %s\n# provider: %s\n# workspace: %s\n\n", meta.Name, meta.StartedAt.UTC().Format(time.RFC3339Nano), meta.Model, meta.Effort, meta.Provider, meta.Workspace)
}

const exchangeMarker = "# exchange "

func (self *Recorder) Start(request req.Request) req.ExchangeObserver {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	sequence := self.next
	self.next++
	exchange := &exchange{recorder: self, sequence: sequence, startedAt: request.StartedAt}
	self.write(fmt.Sprintf("# exchange %d start %s\n> %s %s %s\n", sequence, request.StartedAt.UTC().Format(time.RFC3339Nano), request.Method, censorURL(request.URL), request.Protocol))
	self.writeHeaders(">", request.Header)
	self.write("\n")
	self.write(string(censorBody(request.Body, request.Header.Get("Content-Type"))))
	self.write("\n")
	self.flush()
	return exchange
}

func (self *Recorder) Close() error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.file == nil {
		return nil
	}
	err := self.encoder.Close()
	if err == nil {
		_, err = self.file.Write(trailer(self.next))
	}
	err = errors.Join(err, self.file.Close())
	self.file = nil
	self.encoder = nil
	if err != nil {
		self.fail(err)
	}
	return err
}

func (self *Recorder) writeHeaders(prefix string, headers http.Header) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, value := range headers.Values(name) {
			if isSensitiveName(name) {
				value = redacted
			} else {
				value = censorBearer(value)
			}
			self.write(fmt.Sprintf("%s %s: %s\n", prefix, name, value))
		}
	}
}

func (self *Recorder) write(value string) {
	if self.hasFailed || self.file == nil {
		return
	}
	if _, err := io.WriteString(self.encoder, value); err != nil {
		self.fail(err)
	}
}

func (self *Recorder) flush() {
	if self.hasFailed || self.file == nil {
		return
	}
	if err := self.encoder.Flush(); err != nil {
		self.fail(err)
	}
}

func (self *Recorder) fail(err error) {
	if self.hasFailed {
		return
	}
	self.hasFailed = true
	self.encoder = nil
	if self.file != nil {
		_ = self.file.Close()
		self.file = nil
	}
	if self.report != nil {
		self.report(fmt.Errorf("wire.http.zst recording disabled: %w", err))
	}
}

type exchange struct {
	recorder     *Recorder
	sequence     int
	startedAt    time.Time
	contentType  string
	body         bytes.Buffer
	hasResponded bool
	isStreaming  bool
	lastReadAt   time.Time
}

func (self *exchange) Response(response req.Response) {
	self.recorder.mutex.Lock()
	defer self.recorder.mutex.Unlock()
	self.hasResponded = true
	self.contentType = response.Header.Get("Content-Type")
	self.isStreaming = strings.Contains(strings.ToLower(self.contentType), "event-stream")
	self.recorder.write(fmt.Sprintf("\n< %s %s\n", response.Protocol, response.Status))
	self.recorder.writeHeaders("<", response.Header)
	if response.IsCompressed {
		self.recorder.write(fmt.Sprintf("%s%d gzip decompressed by the transport\n", exchangeMarker, self.sequence))
	}
	self.recorder.write("\n")
	self.recorder.flush()
}

func (self *exchange) Body(readAt time.Time, body []byte) {
	if !self.isStreaming {
		_, _ = self.body.Write(body)
		return
	}

	self.recorder.mutex.Lock()
	defer self.recorder.mutex.Unlock()
	_, _ = self.body.Write(body)
	self.recorder.write(self.readMarker(readAt, len(body)))
	for {
		bufferedBody := self.body.Bytes()
		lineEnd := bytes.IndexByte(bufferedBody, '\n')
		if lineEnd < 0 {
			self.recorder.flush()
			return
		}
		line := bytes.Clone(bufferedBody[:lineEnd+1])
		self.body.Next(lineEnd + 1)
		self.recorder.write(string(censorBody(line, self.contentType)))
	}
}

func (self *exchange) Finish(finishedAt time.Time, err error, isIncomplete bool) {
	self.recorder.mutex.Lock()
	defer self.recorder.mutex.Unlock()
	self.recorder.write(string(censorBody(self.body.Bytes(), self.contentType)))
	self.recorder.write("\n")
	state := "completed"
	switch {
	case isIncomplete:
		state = "incomplete close"
	case errors.Is(err, context.Canceled):
		state = "cancelled"
	case err != nil && !self.hasResponded:
		state = "transport error: " + censorBearer(err.Error())
	case err != nil:
		state = "read error: " + censorBearer(err.Error())
	}
	self.recorder.write(fmt.Sprintf("# exchange %d end %s elapsed=%s %s\n\n", self.sequence, finishedAt.UTC().Format(time.RFC3339Nano), finishedAt.Sub(self.startedAt), state))
	self.recorder.flush()
}

func (self *exchange) readMarker(readAt time.Time, byteCount int) string {
	marker := fmt.Sprintf("%s%d read %s elapsed=%s", exchangeMarker, self.sequence, readAt.UTC().Format(time.RFC3339Nano), readAt.Sub(self.startedAt))
	if !self.lastReadAt.IsZero() {
		marker += " gap=" + readAt.Sub(self.lastReadAt).String()
	}
	self.lastReadAt = readAt

	return marker + fmt.Sprintf(" bytes=%d\n", byteCount)
}

func censorURL(address string) string {
	if address == "" {
		return address
	}

	parsedAddress, err := url.Parse(address)
	if err != nil || parsedAddress.RawQuery == "" {
		return censorBearer(address)
	}

	query, err := url.ParseQuery(parsedAddress.RawQuery)
	if err != nil {
		return censorBearer(address)
	}

	wasCensored := false
	for key := range query {
		if isSensitiveName(key) || isOAuthSecretName(key) {
			query[key] = []string{redacted}
			wasCensored = true
		}
	}
	if !wasCensored {
		return censorBearer(address)
	}

	parsedAddress.RawQuery = query.Encode()
	return censorBearer(parsedAddress.String())
}

func censorBody(body []byte, contentType string) []byte {
	if len(body) == 0 {
		return nil
	}
	lowerType := strings.ToLower(contentType)
	if strings.Contains(lowerType, "json") {
		return censorJSON(body)
	}
	if strings.Contains(lowerType, "x-www-form-urlencoded") {
		values, err := url.ParseQuery(string(body))
		if err == nil {
			for key := range values {
				if isSensitiveName(key) || isOAuthSecretName(key) {
					values[key] = []string{redacted}
				}
			}
			return []byte(values.Encode())
		}
	}
	if strings.Contains(lowerType, "event-stream") {
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			data, found := strings.CutPrefix(line, "data:")
			if !found {
				lines[i] = censorBearer(line)
				continue
			}

			space := ""
			if remainingData, found := strings.CutPrefix(data, " "); found {
				space, data = " ", remainingData
			}
			lines[i] = "data:" + space + string(censorJSON([]byte(data)))
		}
		return []byte(strings.Join(lines, "\n"))
	}
	return []byte(censorBearer(string(body)))
}

func censorJSON(body []byte) []byte {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return []byte(censorBearer(string(body)))
	}

	censoredValue, wasCensored := censorValue(value)
	if !wasCensored {
		return body
	}

	encodedValue, err := json.Marshal(censoredValue)
	if err != nil {
		return []byte(censorBearer(string(body)))
	}
	return encodedValue
}

func censorValue(value any) (any, bool) {
	return censorValueAtDepth(value, 0)
}

func censorValueAtDepth(value any, depth int) (any, bool) {
	switch typedValue := value.(type) {
	case map[string]any:
		wasCensored := false
		for key, item := range typedValue {
			if isSensitiveName(key) || (depth == 0 && isOAuthSecretName(key)) {
				typedValue[key] = redacted
				wasCensored = true
				continue
			}

			censoredItem, itemWasCensored := censorValueAtDepth(item, depth+1)
			typedValue[key] = censoredItem
			wasCensored = wasCensored || itemWasCensored
		}
		return typedValue, wasCensored
	case []any:
		wasCensored := false
		for index, item := range typedValue {
			censoredItem, itemWasCensored := censorValueAtDepth(item, depth+1)
			typedValue[index] = censoredItem
			wasCensored = wasCensored || itemWasCensored
		}
		return typedValue, wasCensored
	case string:
		censoredText := censorBearer(typedValue)
		return censoredText, censoredText != typedValue
	}
	return value, false
}

func isSensitiveName(name string) bool {
	normalisedName := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
	switch normalisedName {
	case
		"authorization",
		"proxyauthorization",
		"cookie",
		"setcookie",
		"apikey",
		"xapikey",
		"openaiaccountid",
		"xopenaiaccountid",
		"chatgptaccountid",
		"account",
		"organization",
		"email",
		"emailaddress",
		"uuid",
		"safetyidentifier",
		"requestid",
		"traceresponse",
		"traceparent",
		"tracestate",
		"cfray",
		"accesstoken",
		"refreshtoken",
		"idtoken",
		"tokenuuid",
		"clientsecret",
		"token",
		"password",
		"codechallenge",
		"codeverifier":
		return true
	}
	return strings.Contains(normalisedName, "credential") ||
		strings.HasSuffix(normalisedName, "accountid") ||
		strings.HasSuffix(normalisedName, "organizationid") ||
		strings.HasSuffix(normalisedName, "workspaceid") ||
		strings.HasSuffix(normalisedName, "requestid")
}

func isOAuthSecretName(name string) bool {
	switch strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name)) {
	case "code", "state":
		return true
	}
	return false
}

func censorBearer(value string) string {
	return bearerPattern.ReplaceAllString(value, "Bearer "+redacted)
}
