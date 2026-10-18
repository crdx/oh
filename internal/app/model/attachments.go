package model

import (
	"slices"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/toolbox/read"
	"crdx.org/oh/pkg/wire/anthropic/messages"
	"crdx.org/oh/pkg/wire/google/generatecontent"
	"crdx.org/oh/pkg/wire/openai/chatcompletions"
	"crdx.org/oh/pkg/wire/openai/responses"
)

var codexModalities = []agent.Modality{agent.ImageModality, agent.PDFModality}

var modalitiesByWire = map[agent.Wire][]agent.Modality{
	agent.CompletionsWire:     chatcompletions.AttachableModalities,
	agent.ResponsesWire:       responses.AttachableModalities,
	agent.MessagesWire:        messages.AttachableModalities,
	agent.GenerateContentWire: generatecontent.AttachableModalities,
}

var attachChecksByWire = map[agent.Wire]func(mediaType string) bool{
	agent.CompletionsWire: chatcompletions.CanAttach,
}

func (self Choice) Attachable() read.Attachable {
	return read.Attachable{Modalities: self.AttachableModalities(), CanAttach: self.canAttach}
}

func (self Choice) canAttach(mediaType string) bool {
	if canAttach, isChecked := attachChecksByWire[self.wire()]; isChecked {
		return canAttach(mediaType)
	}

	return true
}

func (self Choice) AttachableModalities() []agent.Modality {
	wireModalities := self.carriedModalities()

	modelModalities := self.InputModalities
	if modelModalities == nil {
		modelModalities = read.DefaultModalities
	}

	var attachable []agent.Modality
	for _, modality := range wireModalities {
		if slices.Contains(modelModalities, modality) {
			attachable = append(attachable, modality)
		}
	}

	return attachable
}

func (self Choice) carriedModalities() []agent.Modality {
	if self.Provider == CodexProvider {
		return codexModalities
	}

	return modalitiesByWire[self.wire()]
}

func (self Choice) wire() agent.Wire {
	switch self.Provider {
	case CodexProvider:
		return agent.ResponsesWire
	case AnthropicProvider:
		return agent.MessagesWire
	case OllamaProvider:
		return agent.CompletionsWire
	default:
		return self.Wire
	}
}
