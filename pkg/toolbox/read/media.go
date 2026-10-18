package read

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/pkg/agent"
)

const megabyte = 1 << 20

var DefaultLimits = map[agent.Modality]int{
	agent.ImageModality: 20 * megabyte,
	agent.AudioModality: 5 * megabyte,
	agent.VideoModality: 5 * megabyte,
	agent.PDFModality:   5 * megabyte,
}

var oversizeAdvice = map[agent.Modality]string{
	agent.ImageModality: "resize or compress it and read the smaller copy",
	agent.AudioModality: "trim or compress it and read the shorter clip",
	agent.VideoModality: "trim it, lower its resolution, or extract frames as images",
	agent.PDFModality:   "split it or extract the pages needed and read those",
}

const unsniffedMediaType = "application/octet-stream"

var DefaultModalities = []agent.Modality{agent.ImageModality}

var DefaultAttachable = Attachable{Modalities: DefaultModalities}

type Attachable struct {
	Modalities []agent.Modality
	CanAttach  func(mediaType string) bool
	MaxBytes   func(modality agent.Modality) int
}

func (self Attachable) limit(modality agent.Modality) int {
	if self.MaxBytes != nil {
		return self.MaxBytes(modality)
	}

	return DefaultLimits[modality]
}

func (self Attachable) tooLarge(modality agent.Modality) error {
	return fmt.Errorf(
		"%s exceeds the %s limit; %s",
		modalityNouns[modality],
		util.FormatBytes(self.limit(modality), bytePrecision),
		oversizeAdvice[modality],
	)
}

func mediaTypeOf(name string, sniffedType string) (string, bool) {
	if imageutil.IsSupported(sniffedType) {
		return sniffedType, true
	}

	return attachmentType(name, sniffedType)
}

func (self Attachable) takes(modality agent.Modality) bool {
	return slices.Contains(self.Modalities, modality)
}

func (self Attachable) refusal(mediaType string) error {
	modality := agent.ModalityOf(mediaType)
	if !self.takes(modality) {
		return refusal(modality)
	}
	if self.CanAttach != nil && !self.CanAttach(mediaType) {
		return fmt.Errorf("this model's endpoint cannot take %s files", mediaType)
	}

	return nil
}

var sniffedAttachmentTypes = map[string]string{
	"application/pdf": "application/pdf",
	"application/ogg": "audio/ogg",
	"audio/aiff":      "audio/aiff",
	"audio/mpeg":      "audio/mp3",
	"audio/wave":      "audio/wav",
	"video/avi":       "video/avi",
	"video/mp4":       "video/mp4",
	"video/webm":      "video/webm",
}

var attachmentTypesByExtension = map[string]string{
	".pdf":  "application/pdf",
	".aac":  "audio/aac",
	".aif":  "audio/aiff",
	".aiff": "audio/aiff",
	".flac": "audio/flac",
	".mp3":  "audio/mp3",
	".oga":  "audio/ogg",
	".ogg":  "audio/ogg",
	".wav":  "audio/wav",
	".3gp":  "video/3gpp",
	".avi":  "video/avi",
	".flv":  "video/x-flv",
	".m4v":  "video/mp4",
	".mov":  "video/mov",
	".mp4":  "video/mp4",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpg",
	".webm": "video/webm",
	".wmv":  "video/wmv",
}

var modalityNouns = map[agent.Modality]string{
	agent.ImageModality: "image",
	agent.AudioModality: "audio",
	agent.VideoModality: "video",
	agent.PDFModality:   "PDF",
}

func attachmentType(name string, sniffedType string) (string, bool) {
	if mediaType, isKnown := sniffedAttachmentTypes[sniffedType]; isKnown {
		return mediaType, true
	}
	if sniffedType != unsniffedMediaType {
		return "", false
	}

	mediaType, isKnown := attachmentTypesByExtension[strings.ToLower(filepath.Ext(name))]

	return mediaType, isKnown
}

func description(modalities []agent.Modality) string {
	nouns := []string{"text"}
	for _, modality := range agent.Modalities {
		if noun, isNamed := modalityNouns[modality]; isNamed && slices.Contains(modalities, modality) {
			nouns = append(nouns, noun)
		}
	}

	if len(nouns) == 1 {
		return "read a text file"
	}

	return "read a " + strings.Join(nouns[:len(nouns)-1], ", ") + " or " + nouns[len(nouns)-1] + " file"
}

func refusal(modality agent.Modality) error {
	return fmt.Errorf("this model cannot take %s input", modalityNouns[modality])
}
