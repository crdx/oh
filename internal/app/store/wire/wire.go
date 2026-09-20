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

	"crdx.org/io/internal/req"
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
	hasFailed bool
	next      int
	report    func(error)
}

func Open(path string, meta Meta, report func(error)) (*Recorder, error) {
	recorder := &Recorder{report: report, next: 1}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600) //nolint:gosec // the parent store supplies the fixed bundle path
	if err != nil {
		return nil, err
	}
	recorder.file = file

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Size() == 0 {
		if err := writeTranscriptHeader(file, meta); err != nil {
			_ = file.Close()
			return nil, err
		}
	} else {
		next, err := nextExchangeNumber(file, recorder.next)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		recorder.next = next
	}
	return recorder, nil
}

func writeTranscriptHeader(file *os.File, meta Meta) error {
	_, err := fmt.Fprintf(file, "# HTTP transcript\n# session: %s\n# started: %s\n# model: %s\n# effort: %s\n# provider: %s\n# workspace: %s\n\n", meta.Name, meta.StartedAt.UTC().Format(time.RFC3339Nano), meta.Model, meta.Effort, meta.Provider, meta.Workspace)
	return err
}

const exchangeMarker = "# exchange "

const firstTailWindow = 64 << 10

func nextExchangeNumber(file *os.File, next int) (int, error) {
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}

	for window := int64(firstTailWindow); ; window *= 2 {
		at := max(size-window, 0)

		tail := make([]byte, size-at)
		if _, err := file.ReadAt(tail, at); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}

		if sequence, wasFound := lastExchangeNumber(tail, at == 0); wasFound {
			return max(next, sequence+1), nil
		}

		if at == 0 {
			return next, nil
		}
	}
}

func lastExchangeNumber(tail []byte, isWholeFile bool) (int, bool) {
	for at := len(tail); at > 0; {
		lineEnd := at
		lineStart := bytes.LastIndexByte(tail[:lineEnd], '\n') + 1
		at = lineStart - 1

		if lineStart == 0 && !isWholeFile {
			return 0, false
		}

		line := tail[lineStart:lineEnd]
		if !bytes.HasPrefix(line, []byte(exchangeMarker)) {
			continue
		}

		var sequence int
		if _, err := fmt.Sscanf(string(line), exchangeMarker+"%d start", &sequence); err == nil {
			return sequence, true
		}
	}

	return 0, false
}

func (self *Recorder) Start(request req.Request) req.ExchangeObserver {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	sequence := self.next
	self.next++
	exchange := &exchange{recorder: self, sequence: sequence, startedAt: request.StartedAt}
	self.write(fmt.Sprintf("# exchange %d start %s\n> %s %s %s\n", sequence, request.StartedAt.UTC().Format(time.RFC3339Nano), request.Method, request.URL, request.Protocol))
	self.writeHeaders(">", request.Header)
	self.write("\n")
	self.write(string(censorBody(request.Body, request.Header.Get("Content-Type"))))
	self.write("\n")
	return exchange
}

func (self *Recorder) Close() error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.file == nil {
		return nil
	}
	err := self.file.Close()
	self.file = nil
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
	if _, err := self.file.WriteString(value); err != nil {
		self.fail(err)
	}
}

func (self *Recorder) fail(err error) {
	if self.hasFailed {
		return
	}
	self.hasFailed = true
	if self.file != nil {
		_ = self.file.Close()
		self.file = nil
	}
	if self.report != nil {
		self.report(fmt.Errorf("wire.http recording disabled: %w", err))
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
}

func (self *exchange) readMarker(readAt time.Time, byteCount int) string {
	marker := fmt.Sprintf("%s%d read %s elapsed=%s", exchangeMarker, self.sequence, readAt.UTC().Format(time.RFC3339Nano), readAt.Sub(self.startedAt))
	if !self.lastReadAt.IsZero() {
		marker += " gap=" + readAt.Sub(self.lastReadAt).String()
	}
	self.lastReadAt = readAt

	return marker + fmt.Sprintf(" bytes=%d\n", byteCount)
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
				if isSensitiveName(key) {
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
	switch typedValue := value.(type) {
	case map[string]any:
		wasCensored := false
		for key, item := range typedValue {
			if isSensitiveName(key) {
				typedValue[key] = redacted
				wasCensored = true
				continue
			}

			censoredItem, itemWasCensored := censorValue(item)
			typedValue[key] = censoredItem
			wasCensored = wasCensored || itemWasCensored
		}
		return typedValue, wasCensored
	case []any:
		wasCensored := false
		for index, item := range typedValue {
			censoredItem, itemWasCensored := censorValue(item)
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
		"password":
		return true
	}
	return strings.Contains(normalisedName, "credential") ||
		strings.HasSuffix(normalisedName, "accountid") ||
		strings.HasSuffix(normalisedName, "organizationid") ||
		strings.HasSuffix(normalisedName, "workspaceid") ||
		strings.HasSuffix(normalisedName, "requestid")
}

func censorBearer(value string) string {
	return bearerPattern.ReplaceAllString(value, "Bearer "+redacted)
}
