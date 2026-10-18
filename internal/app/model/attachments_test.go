package model

import (
	"slices"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestASessionAttachesWhatBothItsModelAndItsWireTake(t *testing.T) {
	everything := []agent.Modality{
		agent.TextModality,
		agent.ImageModality,
		agent.AudioModality,
		agent.VideoModality,
		agent.PDFModality,
	}

	for name, test := range map[string]struct {
		choice Choice
		want   []agent.Modality
	}{
		"an Anthropic model reading PDFs": {
			choice: Choice{Provider: AnthropicProvider, InputModalities: everything},
			want:   []agent.Modality{agent.ImageModality, agent.PDFModality},
		},
		"a Codex model, whose endpoint takes images and PDFs": {
			choice: Choice{Provider: CodexProvider, InputModalities: everything},
			want:   []agent.Modality{agent.ImageModality, agent.PDFModality},
		},
		"a Gemini model over OpenCode Go": {
			choice: Choice{
				Provider:        OpencodeGoProvider,
				Wire:            agent.GenerateContentWire,
				InputModalities: everything,
			},
			want: []agent.Modality{agent.ImageModality, agent.AudioModality, agent.VideoModality, agent.PDFModality},
		},
		"a model taking video over a wire that cannot carry it": {
			choice: Choice{
				Provider:        OpencodeGoProvider,
				Wire:            agent.MessagesWire,
				InputModalities: []agent.Modality{agent.TextModality, agent.ImageModality, agent.VideoModality},
			},
			want: []agent.Modality{agent.ImageModality},
		},
		"a model taking audio and video over Chat Completions": {
			choice: Choice{
				Provider:        OpencodeGoProvider,
				Wire:            agent.CompletionsWire,
				InputModalities: []agent.Modality{agent.TextModality, agent.AudioModality, agent.VideoModality},
			},
			want: []agent.Modality{agent.AudioModality, agent.VideoModality},
		},
		"a text-only model": {
			choice: Choice{Provider: OllamaProvider, InputModalities: []agent.Modality{agent.TextModality}},
			want:   nil,
		},
		"a model nobody describes, which keeps taking images": {
			choice: Choice{Provider: AnthropicProvider},
			want:   []agent.Modality{agent.ImageModality},
		},
	} {
		if got := test.choice.AttachableModalities(); !slices.Equal(got, test.want) {
			t.Errorf("%s: got %v, want %v", name, got, test.want)
		}
	}
}

func TestAWireRefusesTheFormatsItCannotCarry(t *testing.T) {
	completions := Choice{Provider: OpencodeGoProvider, Wire: agent.CompletionsWire}.Attachable()
	for mediaType, want := range map[string]bool{
		"audio/mp3":       true,
		"audio/wav":       true,
		"audio/flac":      false,
		"video/mp4":       true,
		"application/pdf": true,
	} {
		if got := completions.CanAttach(mediaType); got != want {
			t.Errorf("Chat Completions carrying %s: got %t, want %t", mediaType, got, want)
		}
	}

	if !(Choice{Provider: OpencodeGoProvider, Wire: agent.GenerateContentWire}).Attachable().CanAttach("audio/flac") {
		t.Error("expected Generate Content to carry FLAC")
	}
}
