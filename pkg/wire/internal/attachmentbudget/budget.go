package attachmentbudget

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

const EncodedBytes = 15_000_000

type Attachment struct {
	MediaType    string
	EncodedBytes int
}

type Shape struct {
	Markers [][]byte
	Measure func(object map[string]any) (Attachment, bool)
	Replace func(notice string) any
}

type Budget struct {
	refusals map[int]string
}

type found struct {
	item       int
	attachment Attachment
}

func (self *Budget) Reset() {
	self.refusals = nil
}

func (self *Budget) Trim(items []json.RawMessage, shape Shape) []json.RawMessage {
	trees := map[int]any{}

	var attachments []found

	for index, item := range items {
		if !mayHold(item, shape) {
			continue
		}

		var tree any
		if json.Unmarshal(item, &tree) != nil {
			continue
		}

		trees[index] = tree
		if _, isRefused := self.refusals[index]; isRefused {
			continue
		}
		visit(tree, shape, func(attachment Attachment) any {
			attachments = append(attachments, found{item: index, attachment: attachment})
			return nil
		})
	}

	keptFrom := len(attachments)
	total := 0
	for position, candidate := range slices.Backward(attachments) {
		total += candidate.attachment.EncodedBytes
		if total > EncodedBytes && position < len(attachments)-1 {
			break
		}
		keptFrom = position
	}
	if keptFrom == 0 && len(self.refusals) == 0 {
		return items
	}

	trimmedItems := slices.Clone(items)
	evictedItems := map[int]bool{}
	for _, candidate := range attachments[:keptFrom] {
		evictedItems[candidate.item] = true
	}

	position := 0
	for index := range items {
		tree, isDecoded := trees[index]
		if !isDecoded {
			continue
		}

		if reason, isRefused := self.refusals[index]; isRefused {
			visit(tree, shape, func(attachment Attachment) any {
				return shape.Replace(agent.RefusedAttachmentNotice(attachment.MediaType, reason))
			})
		} else {
			if !evictedItems[index] {
				visit(tree, shape, func(Attachment) any {
					position++
					return nil
				})
				continue
			}

			visit(tree, shape, func(attachment Attachment) any {
				isEvicted := position < keptFrom
				position++
				if !isEvicted {
					return nil
				}
				return shape.Replace(agent.RemovedAttachmentNotice(attachment.MediaType))
			})
		}

		if trimmedItem, err := json.Marshal(tree); err == nil {
			trimmedItems[index] = trimmedItem
		}
	}

	return trimmedItems
}

func (self *Budget) Refuse(items []json.RawMessage, shape Shape, err error) (func(), bool) {
	reason, isRefusal := refusalOf(err)
	if !isRefusal {
		return nil, false
	}

	for index, item := range slices.Backward(items) {
		if _, isRefused := self.refusals[index]; isRefused || !holdsAttachment(item, shape) {
			continue
		}

		if self.refusals == nil {
			self.refusals = map[int]string{}
		}
		self.refusals[index] = reason

		return func() { delete(self.refusals, index) }, true
	}

	return nil, false
}

func mayHold(item json.RawMessage, shape Shape) bool {
	return slices.ContainsFunc(shape.Markers, func(marker []byte) bool { return bytes.Contains(item, marker) })
}

func holdsAttachment(item json.RawMessage, shape Shape) bool {
	if !mayHold(item, shape) {
		return false
	}

	var tree any
	if json.Unmarshal(item, &tree) != nil {
		return false
	}

	isHeld := false
	visit(tree, shape, func(Attachment) any {
		isHeld = true
		return nil
	})

	return isHeld
}

var refusingStatuses = []int{
	http.StatusBadRequest,
	http.StatusRequestEntityTooLarge,
	http.StatusUnprocessableEntity,
}

const maximumReasonBytes = 300

func refusalOf(err error) (string, bool) {
	statusError, isStatus := errors.AsType[*req.StatusError](err)
	if !isStatus || !slices.Contains(refusingStatuses, statusError.Status) {
		return "", false
	}

	reason := fmt.Sprintf("HTTP %d", statusError.Status)
	if message := statusError.Message; message != "" {
		if len(message) > maximumReasonBytes {
			message = strings.ToValidUTF8(message[:maximumReasonBytes], "") + "…"
		}
		reason += ": " + message
	}

	return reason, true
}

func visit(value any, shape Shape, onAttachment func(Attachment) any) {
	switch typedValue := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(typedValue) {
			typedValue[key] = visitChild(typedValue[key], shape, onAttachment)
		}
	case []any:
		for index, element := range typedValue {
			typedValue[index] = visitChild(element, shape, onAttachment)
		}
	}
}

func visitChild(value any, shape Shape, onAttachment func(Attachment) any) any {
	if object, isObject := value.(map[string]any); isObject {
		if attachment, isAttachment := shape.Measure(object); isAttachment {
			if replacement := onAttachment(attachment); replacement != nil {
				return replacement
			}
			return value
		}
	}

	visit(value, shape, onAttachment)

	return value
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}
