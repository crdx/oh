package harness

import (
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/toolbox/read"
)

var attachmentLimitToggles = map[agent.Modality]experimental.Name{
	agent.ImageModality: experimental.ImageAttachmentLimit,
	agent.AudioModality: experimental.AudioAttachmentLimit,
	agent.VideoModality: experimental.VideoAttachmentLimit,
	agent.PDFModality:   experimental.PDFAttachmentLimit,
}

func attachableFor(choice model.Choice, toggles *experimental.Toggles) read.Attachable {
	attachable := choice.Attachable()
	attachable.MaxBytes = func(modality agent.Modality) int {
		return toggles.GetSize(attachmentLimitToggles[modality], read.DefaultLimits[modality])
	}

	return attachable
}
