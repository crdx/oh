package paste

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	"crdx.org/io/internal/app/key"
	"crdx.org/io/internal/util"
)

const (
	eventName     = "Paste event"
	TextMediaType = "text/plain"
	maxBytes      = 32 << 20
)

var ImageMediaTypes = []string{
	"image/png",
	"image/jpeg",
	"image/webp",
	"image/gif",
}

type Kind int

const (
	Ignored Kind = iota
	Requested
	Text
	Image
	Failed
)

type Result struct {
	Kind      Kind
	Sequence  string
	Text      string
	MediaType string
	Data      []byte
	Message   string
}

type Exchange struct {
	password  string
	location  string
	mediaType string
	payload   []byte
}

func (self *Exchange) Receive(report *key.ClipboardReport) Result {
	switch report.Status {
	case key.ClipboardOpened:
		self.password = report.Password
		self.location = report.Location
		self.discard()

	case key.ClipboardChunk:
		if report.MediaType == key.MediaTypeList {
			return self.request(string(report.Payload))
		}
		return self.collect(report)

	case key.ClipboardClosed:
		return self.settle()

	case key.ClipboardFailed:
		self.discard()
		return Result{Kind: Failed, Message: failureMessage(report.Failure)}

	case key.ClipboardUnknown:
	}

	return Result{}
}

func (self *Exchange) discard() {
	self.mediaType = ""
	self.payload = nil
}

func (self *Exchange) request(list string) Result {
	mediaType, isFound := choose(strings.Fields(list))
	if !isFound {
		return Result{}
	}

	return Result{Kind: Requested, Sequence: readSequence(self.location, self.password, mediaType)}
}

func (self *Exchange) collect(report *key.ClipboardReport) Result {
	if len(self.payload)+len(report.Payload) > maxBytes {
		self.discard()

		return Result{
			Kind:    Failed,
			Message: "the clipboard holds more than " + util.FormatBytes(maxBytes, 0),
		}
	}

	self.mediaType = report.MediaType
	self.payload = append(self.payload, report.Payload...)

	return Result{}
}

func (self *Exchange) settle() Result {
	mediaType, payload := self.mediaType, self.payload
	self.discard()

	switch {
	case mediaType == TextMediaType:
		return Result{Kind: Text, Text: string(payload)}
	case slices.Contains(ImageMediaTypes, mediaType):
		return Result{Kind: Image, MediaType: mediaType, Data: payload}
	}

	return Result{}
}

func choose(available []string) (string, bool) {
	for _, mediaType := range ImageMediaTypes {
		if slices.Contains(available, mediaType) {
			return mediaType, true
		}
	}

	if slices.Contains(available, TextMediaType) {
		return TextMediaType, true
	}

	return "", false
}

func readSequence(location string, password string, mediaType string) string {
	metadata := "type=read"

	if location != "" {
		metadata += ":loc=" + location
	}

	if password != "" {
		metadata += ":pw=" + encode(password) + ":name=" + encode(eventName)
	}

	return fmt.Sprintf("\x1b]%s;%s;%s\x1b\\", key.ClipboardCode, metadata, encode(mediaType))
}

func encode(text string) string {
	return base64.StdEncoding.EncodeToString([]byte(text))
}

func failureMessage(failure string) string {
	switch failure {
	case "EPERM":
		return "permission to read the clipboard was refused"
	case "ENOSYS":
		return "the terminal has no clipboard to read"
	case "EBUSY":
		return "another window is using the clipboard"
	}

	return "the terminal refused the clipboard with " + failure
}
