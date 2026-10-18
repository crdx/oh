package read_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/read"
)

const pdfContent = "%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"

var everyModality = []agent.Modality{
	agent.ImageModality,
	agent.AudioModality,
	agent.VideoModality,
	agent.PDFModality,
}

func readAccepting(
	t *testing.T,
	root *file.Root,
	modalities []agent.Modality,
	arguments string,
) (tool.ToolCallResult, error) {
	t.Helper()

	call, err := read.NewAccepting(root, file.NewSnapshots(), read.Attachable{Modalities: modalities}).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec(t.Context())
}

func TestAPDFIsAttachedForAModelThatTakesThem(t *testing.T) {
	root := testRoot(t, "paper.pdf", pdfContent)

	result, err := readAccepting(t, root, everyModality, `{"path":"paper.pdf"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output != "application/pdf, 29B" {
		t.Errorf("expected a PDF description, got %q", result.Output)
	}
	if result.Attachment.Name != "paper.pdf" || result.Attachment.MediaType != "application/pdf" ||
		string(result.Attachment.Data) != pdfContent {
		t.Errorf("got attachment %+v", result.Attachment)
	}
	if result.Metrics.Kind != tool.MetricAttachment || result.Metrics.Bytes != int64(len(pdfContent)) {
		t.Errorf("got metrics %#v", result.Metrics)
	}
}

func TestEachKindOfMediaIsRecognised(t *testing.T) {
	for name, test := range map[string]struct {
		content   string
		mediaType string
	}{
		"talk.mp3":   {content: "ID3\x04\x00\x00\x00\x00\x00\x00", mediaType: "audio/mp3"},
		"bare.mp3":   {content: "\xff\xfb\x90\x00\x00\x00", mediaType: "audio/mp3"},
		"tone.wav":   {content: "RIFF\x24\x00\x00\x00WAVEfmt ", mediaType: "audio/wav"},
		"song.flac":  {content: "fLaC\x00\x00\x00\x22", mediaType: "audio/flac"},
		"clip.mp4":   {content: "\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom", mediaType: "video/mp4"},
		"clip.webm":  {content: "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01", mediaType: "video/webm"},
		"movie.mov":  {content: "\x00\x00\x00\x14ftypqt  \x00\x00\x00\x00qt  ", mediaType: "video/mov"},
		"report.PDF": {content: pdfContent, mediaType: "application/pdf"},
	} {
		root := testRoot(t, name, test.content)

		result, err := readAccepting(t, root, everyModality, `{"path":"`+name+`"}`)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
			continue
		}
		if result.Attachment.MediaType != test.mediaType {
			t.Errorf("%s: got media type %q, want %q", name, result.Attachment.MediaType, test.mediaType)
		}
	}
}

func TestMediaTheModelCannotTakeIsRefused(t *testing.T) {
	for name, test := range map[string]struct {
		content string
		want    string
	}{
		"paper.pdf":   {content: pdfContent, want: "this model cannot take PDF input"},
		"talk.mp3":    {content: "ID3\x04\x00\x00\x00\x00\x00\x00", want: "this model cannot take audio input"},
		"clip.webm":   {content: "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01", want: "this model cannot take video input"},
		"picture.png": {content: "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24), want: "this model cannot take image input"},
	} {
		root := testRoot(t, name, test.content)

		result, err := readAccepting(t, root, nil, `{"path":"`+name+`"}`)
		if err == nil || err.Error() != test.want {
			t.Errorf("%s: got error %v, want %q", name, err, test.want)
		}
		if result.Attachment.IsPresent() || len(result.Image.Data) > 0 {
			t.Errorf("%s: expected nothing attached, got %+v", name, result)
		}
	}
}

func TestTheDefaultReadToolTakesImagesAlone(t *testing.T) {
	root := testRoot(t, "paper.pdf", pdfContent)

	_, err := exec(t, root, `{"path":"paper.pdf"}`)
	if err == nil || err.Error() != "this model cannot take PDF input" {
		t.Errorf("expected the default read tool to refuse a PDF, got %v", err)
	}
}

func TestMediaCannotBeReadAsLines(t *testing.T) {
	root := testRoot(t, "paper.pdf", pdfContent)

	_, err := readAccepting(t, root, everyModality, `{"path":"paper.pdf","offset":2}`)
	if err == nil || err.Error() != "PDF files do not support line ranges" {
		t.Errorf("got error %v", err)
	}
}

func TestMediaLargerThanAnAttachmentCanBeIsRefused(t *testing.T) {
	directory := t.TempDir()
	for name, size := range map[string]int{"big.mp3": 11 * 1024 * 1024, "huge.mp3": 21 * 1024 * 1024} {
		if err := os.WriteFile(filepath.Join(directory, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	opened, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	root := file.New(opened, allowAll)

	for _, name := range []string{"big.mp3", "huge.mp3"} {
		_, err := readAccepting(t, root, everyModality, `{"path":"`+name+`"}`)
		if err == nil || err.Error() != "audio exceeds the 5M limit; trim or compress it and read the shorter clip" {
			t.Errorf("%s: got error %v", name, err)
		}
	}

	_, err = readAccepting(t, root, nil, `{"path":"huge.mp3"}`)
	if err == nil || err.Error() != "this model cannot take audio input" {
		t.Errorf("expected an unaccepted kind to be refused before its size, got %v", err)
	}
}

func TestTheDescriptionNamesOnlyWhatTheModelTakes(t *testing.T) {
	root := testRoot(t, "notes.txt", "notes")

	for _, test := range []struct {
		modalities []agent.Modality
		want       string
	}{
		{modalities: nil, want: "read a text file"},
		{modalities: read.DefaultModalities, want: "read a text or image file"},
		{modalities: []agent.Modality{agent.PDFModality, agent.ImageModality}, want: "read a text, image or PDF file"},
		{modalities: everyModality, want: "read a text, image, audio, video or PDF file"},
	} {
		attachable := read.Attachable{Modalities: test.modalities}
		if got := read.NewAccepting(root, file.NewSnapshots(), attachable).Description(); got != test.want {
			t.Errorf("got %q, want %q", got, test.want)
		}
	}
}

func TestAFormatTheEndpointCannotCarryIsRefused(t *testing.T) {
	root := testRoot(t, "song.flac", "fLaC\x00\x00\x00\x22")
	attachable := read.Attachable{
		Modalities: everyModality,
		CanAttach:  func(mediaType string) bool { return mediaType != "audio/flac" },
	}

	call, err := read.NewAccepting(root, file.NewSnapshots(), attachable).Parse(`{"path":"song.flac"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); err == nil || err.Error() != "this model's endpoint cannot take audio/flac files" {
		t.Errorf("got error %v", err)
	}
}

func TestEachKindOfMediaHasItsOwnLimit(t *testing.T) {
	directory := t.TempDir()
	large := append([]byte(pdfContent), make([]byte, 21*1024*1024)...)
	if err := os.WriteFile(filepath.Join(directory, "large.pdf"), large, 0o600); err != nil {
		t.Fatal(err)
	}
	picture := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24)
	if err := os.WriteFile(filepath.Join(directory, "picture.png"), []byte(picture), 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	root := file.New(opened, allowAll)

	limits := map[agent.Modality]int{agent.PDFModality: 30 * 1024 * 1024, agent.ImageModality: 16}
	attachable := read.Attachable{
		Modalities: everyModality,
		MaxBytes:   func(modality agent.Modality) int { return limits[modality] },
	}

	call, err := read.NewAccepting(root, file.NewSnapshots(), attachable).Parse(`{"path":"large.pdf"}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("expected a raised PDF limit to read past the file limit, got %v", err)
	}
	if len(result.Attachment.Data) != len(large) {
		t.Errorf("got %d bytes, want %d", len(result.Attachment.Data), len(large))
	}

	call, err = read.NewAccepting(root, file.NewSnapshots(), attachable).Parse(`{"path":"picture.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "image exceeds the 16B limit; resize or compress it and read the smaller copy"
	if _, err := call.Exec(t.Context()); err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}
