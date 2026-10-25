package toolresult

import (
	"bytes"
	"encoding/json"
	"strings"

	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
)

var markupEscapes = map[string]byte{
	"u0026": '&',
	"u003c": '<',
	"u003e": '>',
}

func render(exchange toolresult.Exchange) string {
	var parts []string
	if exchange.Request.Name != "" {
		parts = append(parts, style.Heading(safe(exchange.Request.Name)))
	}
	var arguments bytes.Buffer
	if err := json.Indent(&arguments, []byte(exchange.Request.Arguments), "", "    "); err != nil {
		arguments.WriteString(exchange.Request.Arguments)
	}
	if arguments.Len() > 0 {
		parts = append(parts, markdown.HighlightText(safe(unescapeMarkup(arguments.String())), "json"))
	}
	if exchange.Result.Text != "" {
		parts = append(parts, safe(exchange.Result.Text))
	}
	return strings.Join(parts, "\n\n")
}

func renderTask(snapshot tasks.Snapshot) string {
	exchange := toolresult.Exchange{
		Request: agent.Event{Name: snapshot.ToolName},
		Result:  agent.Event{Text: snapshot.Output},
	}
	if snapshot.Invocation != nil {
		exchange.Request.Arguments = snapshot.Invocation.Arguments
	}
	return render(exchange)
}

func unescapeMarkup(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' || i+1 >= len(text) {
			out.WriteByte(text[i])
			continue
		}
		if character, isMarkup := markupEscapes[text[i+1:min(i+6, len(text))]]; isMarkup {
			out.WriteByte(character)
			i += 5
			continue
		}
		out.WriteString(text[i : i+2])
		i++
	}
	return out.String()
}

func safe(text string) string {
	return strutil.StripControl(text)
}
