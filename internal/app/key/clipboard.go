package key

import (
	"encoding/base64"
	"strings"
)

const (
	ClipboardCode = "5522"
	MediaTypeList = "."
)

type ClipboardStatus int

const (
	ClipboardUnknown ClipboardStatus = iota
	ClipboardOpened
	ClipboardChunk
	ClipboardClosed
	ClipboardFailed
)

type ClipboardReport struct {
	Status    ClipboardStatus
	Failure   string
	MediaType string
	Password  string
	Location  string
	Payload   []byte
}

func clipboardReport(command string) (Key, bool) {
	code, remainder, hasRemainder := strings.Cut(command, ";")
	if code != ClipboardCode || !hasRemainder {
		return Key{}, false
	}

	metadata, payload, _ := strings.Cut(remainder, ";")

	report := ClipboardReport{}
	isRead := false

	for field := range strings.SplitSeq(metadata, ":") {
		name, value, _ := strings.Cut(field, "=")

		switch name {
		case "type":
			isRead = value == "read"
		case "status":
			report.Status, report.Failure = clipboardStatus(value)
		case "mime":
			report.MediaType = decodeText(value)
		case "pw":
			report.Password = decodeText(value)
		case "loc":
			report.Location = value
		}
	}

	if !isRead || report.Status == ClipboardUnknown {
		return Key{}, false
	}

	report.Payload, _ = base64.StdEncoding.DecodeString(payload)

	return Key{Code: Clipboard, Clipboard: &report}, true
}

func clipboardStatus(value string) (ClipboardStatus, string) {
	switch value {
	case "":
		return ClipboardUnknown, ""
	case "OK":
		return ClipboardOpened, ""
	case "DATA":
		return ClipboardChunk, ""
	case "DONE":
		return ClipboardClosed, ""
	}

	return ClipboardFailed, value
}

func decodeText(value string) string {
	decodedText, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return ""
	}

	return string(decodedText)
}
