package attachmentbudget

import (
	"encoding/base64"
	"strings"

	"crdx.org/oh/pkg/agent"
)

var Messages = Shape{
	Markers: [][]byte{[]byte(`"document"`)},
	Measure: func(object map[string]any) (Attachment, bool) {
		source, _ := object["source"].(map[string]any)
		if object["type"] != "document" || source == nil {
			return Attachment{}, false
		}

		return encodedAttachment(source["media_type"], source["data"])
	},
	Replace: func(notice string) any {
		return map[string]any{"type": "text", "text": notice}
	},
}

var Responses = Shape{
	Markers: [][]byte{[]byte(`"input_file"`)},
	Measure: func(object map[string]any) (Attachment, bool) {
		if object["type"] != "input_file" {
			return Attachment{}, false
		}

		return dataURLAttachment(object["file_data"])
	},
	Replace: func(notice string) any {
		return map[string]any{"type": "input_text", "text": notice}
	},
}

var Completions = Shape{
	Markers: [][]byte{[]byte(`"file_data"`), []byte(`"input_audio"`), []byte(`"video_url"`)},
	Measure: func(object map[string]any) (Attachment, bool) {
		file, _ := object["file"].(map[string]any)
		audio, _ := object["input_audio"].(map[string]any)
		video, _ := object["video_url"].(map[string]any)
		switch {
		case object["type"] == "file" && file != nil:
			return dataURLAttachment(file["file_data"])
		case object["type"] == "input_audio" && audio != nil:
			format, _ := audio["format"].(string)
			return encodedAttachment("audio/"+format, audio["data"])
		case object["type"] == "video_url" && video != nil:
			return dataURLAttachment(video["url"])
		default:
			return Attachment{}, false
		}
	},
	Replace: func(notice string) any {
		return map[string]any{"type": "text", "text": notice}
	},
}

const replacedMediaType = "text/plain"

var GenerateContent = Shape{
	Markers: [][]byte{[]byte(`"inlineData"`)},
	Measure: func(object map[string]any) (Attachment, bool) {
		blob, _ := object["inlineData"].(map[string]any)
		if blob == nil {
			return Attachment{}, false
		}

		return encodedAttachment(blob["mimeType"], blob["data"])
	},
	Replace: func(notice string) any {
		return map[string]any{"inlineData": map[string]any{
			"mimeType": replacedMediaType,
			"data":     base64.StdEncoding.EncodeToString([]byte(notice)),
		}}
	},
}

func encodedAttachment(mediaTypeValue any, dataValue any) (Attachment, bool) {
	mediaType, _ := mediaTypeValue.(string)
	data, isData := dataValue.(string)
	if !isData || !isBudgeted(mediaType) {
		return Attachment{}, false
	}

	return Attachment{MediaType: mediaType, EncodedBytes: len(data)}, true
}

func dataURLAttachment(value any) (Attachment, bool) {
	address, _ := value.(string)
	header, data, isDataURL := strings.Cut(strings.TrimPrefix(address, "data:"), ";base64,")
	if !strings.HasPrefix(address, "data:") || !isDataURL {
		return Attachment{}, false
	}

	return encodedAttachment(header, data)
}

func isBudgeted(mediaType string) bool {
	modality := agent.ModalityOf(mediaType)

	return modality == agent.PDFModality || modality == agent.AudioModality || modality == agent.VideoModality
}
