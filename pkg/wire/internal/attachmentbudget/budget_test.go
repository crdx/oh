package attachmentbudget

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

func quoted(text string) string {
	return strconv.Quote(text)
}

func removedText(mediaType string) string {
	return quoted(agent.RemovedAttachmentNotice(mediaType))
}

func documentItem(name string, size int) json.RawMessage {
	return json.RawMessage(`{"role":"user","content":[{"type":"text","text":"` + name + `"},` +
		`{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` +
		strings.Repeat("A", size) + `"}}]}`)
}

func TestAttachmentsWithinTheBudgetAreLeftExactlyAsStored(t *testing.T) {
	items := []json.RawMessage{
		documentItem("first", 6_000_000),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"read"}]}`),
		documentItem("second", 6_000_000),
	}

	trimmed := (&Budget{}).Trim(items, Messages)
	if &trimmed[0] != &items[0] {
		t.Error("expected history within the budget to be returned untouched")
	}
}

func TestTheOldestAttachmentsAreLeftOutOnceTheBudgetIsSpent(t *testing.T) {
	items := []json.RawMessage{
		documentItem("first", 6_000_000),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"read"}]}`),
		documentItem("second", 6_000_000),
		documentItem("third", 6_000_000),
	}

	trimmed := (&Budget{}).Trim(items, Messages)

	if strings.Contains(string(trimmed[0]), `"document"`) {
		t.Errorf("expected the oldest PDF to be left out, got %.200s", trimmed[0])
	}
	if !strings.Contains(string(trimmed[0]), `{"text":`+removedText("application/pdf")+`,"type":"text"}`) ||
		!strings.Contains(string(trimmed[0]), `"text":"first"`) {
		t.Errorf("expected the oldest PDF to give way to a notice beside its text, got %.200s", trimmed[0])
	}
	for _, index := range []int{1, 2, 3} {
		if string(trimmed[index]) != string(items[index]) {
			t.Errorf("item %d changed although it fits: %.200s", index, trimmed[index])
		}
	}
	if !strings.Contains(string(items[0]), `"document"`) {
		t.Error("expected the stored history to keep the PDF")
	}
}

func TestEveryWireShapeIsMeasuredAndReplaced(t *testing.T) {
	data := strings.Repeat("A", 8_000_000)
	for name, test := range map[string]struct {
		shape Shape
		item  string
		want  string
	}{
		"responses": {
			shape: Responses,
			item:  `{"type":"function_call_output","output":[{"type":"input_file","filename":"a.pdf","file_data":"data:application/pdf;base64,` + data + `"}]}`,
			want:  `{"text":` + removedText("application/pdf") + `,"type":"input_text"}`,
		},
		"completions": {
			shape: Completions,
			item:  `{"role":"user","content":[{"type":"file","file":{"filename":"a.pdf","file_data":"data:application/pdf;base64,` + data + `"}}]}`,
			want:  `{"text":` + removedText("application/pdf") + `,"type":"text"}`,
		},
		"completions audio": {
			shape: Completions,
			item:  `{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"` + data + `","format":"mp3"}}]}`,
			want:  `{"text":` + removedText("audio/mp3") + `,"type":"text"}`,
		},
		"completions video": {
			shape: Completions,
			item:  `{"role":"user","content":[{"type":"video_url","video_url":{"url":"data:video/mp4;base64,` + data + `"}}]}`,
			want:  `{"text":` + removedText("video/mp4") + `,"type":"text"}`,
		},
		"generate content": {
			shape: GenerateContent,
			item:  `{"role":"user","parts":[{"inlineData":{"mimeType":"audio/mp3","data":"` + data + `"}}]}`,
			want:  `{"inlineData":{"data":"` + base64.StdEncoding.EncodeToString([]byte(agent.RemovedAttachmentNotice("audio/mp3"))) + `","mimeType":"text/plain"}}`,
		},
	} {
		items := []json.RawMessage{json.RawMessage(test.item), json.RawMessage(test.item)}

		trimmed := (&Budget{}).Trim(items, test.shape)
		if !strings.Contains(string(trimmed[0]), test.want) {
			t.Errorf("%s: expected %s, got %.300s", name, test.want, trimmed[0])
		}
		if string(trimmed[1]) != test.item {
			t.Errorf("%s: expected the newest attachment to be kept", name)
		}
	}
}

func TestImagesAreNotCountedAgainstTheBudget(t *testing.T) {
	image := `{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"` + strings.Repeat("A", 8_000_000) + `"}}]}`
	items := []json.RawMessage{json.RawMessage(image), json.RawMessage(image), json.RawMessage(image)}

	trimmed := (&Budget{}).Trim(items, GenerateContent)
	if &trimmed[0] != &items[0] {
		t.Error("expected images to be left to their own bounding")
	}
}

func TestTheNewestAttachmentIsSentHoweverLargeItIs(t *testing.T) {
	items := []json.RawMessage{documentItem("first", 1_000), documentItem("huge", 16_000_000)}

	trimmed := (&Budget{}).Trim(items, Messages)
	if string(trimmed[1]) != string(items[1]) {
		t.Error("expected the newest attachment to be sent whatever its size")
	}
	if strings.Contains(string(trimmed[0]), `"document"`) {
		t.Error("expected everything older than an attachment past the budget to be left out")
	}
}

func TestARefusedAttachmentIsLeftOutWithTheEndpointsReason(t *testing.T) {
	items := []json.RawMessage{
		documentItem("first", 1_000),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"read"}]}`),
		documentItem("second", 1_000),
	}
	refusal := &req.StatusError{Status: 400, Message: "A maximum of 100 PDF pages may be provided."}

	var budget Budget
	restore, isRefused := budget.Refuse(items, Messages, refusal)
	if !isRefused {
		t.Fatal("expected a refused request to drop its newest attachment")
	}

	trimmed := budget.Trim(items, Messages)
	notice := quoted(agent.RefusedAttachmentNotice(
		"application/pdf", "HTTP 400: A maximum of 100 PDF pages may be provided.",
	))
	if !strings.Contains(string(trimmed[2]), notice) || strings.Contains(string(trimmed[2]), `"document"`) {
		t.Errorf("expected the newest PDF to give way to the refusal, got %.300s", trimmed[2])
	}
	if string(trimmed[0]) != string(items[0]) {
		t.Errorf("expected the older PDF to stay, got %.300s", trimmed[0])
	}

	if _, isRefusedAgain := budget.Refuse(items, Messages, refusal); !isRefusedAgain {
		t.Error("expected a second refusal to drop the next older attachment")
	}

	restore()
	if trimmed := budget.Trim(items, Messages); !strings.Contains(string(trimmed[2]), `"document"`) {
		t.Errorf("expected restoring to bring the PDF back, got %.300s", trimmed[2])
	}
}

func TestOnlyARequestRefusedOutrightCountsAgainstItsAttachment(t *testing.T) {
	items := []json.RawMessage{documentItem("first", 1_000)}

	for name, err := range map[string]error{
		"no error":     nil,
		"server error": &req.StatusError{Status: 500},
		"rate limit":   &req.StatusError{Status: 429},
	} {
		var budget Budget
		if _, isRefused := budget.Refuse(items, Messages, err); isRefused {
			t.Errorf("%s: expected the attachment to stay", name)
		}
	}

	var budget Budget
	if _, isRefused := budget.Refuse([]json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)}, Messages,
		&req.StatusError{Status: 400}); isRefused {
		t.Error("expected a history without attachments to have nothing to drop")
	}
}
