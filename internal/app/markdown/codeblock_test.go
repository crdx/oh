package markdown_test

import (
	"reflect"
	"testing"

	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/style"
)

func TestCodeBlockKeepsIndentedColumnsAndAnEmbeddedFence(t *testing.T) {
	body := "Usage:\n  tool   --flag    meaning\n````\n  other  --longer  detail"
	block := markdown.CodeBlock("", body)
	if want := "`````\n" + body + "\n`````"; block != want {
		t.Errorf("got %q, want %q", block, want)
	}

	var rows []string
	for _, row := range markdown.Render(block, 80) {
		rows = append(rows, style.Plain(row))
	}
	if want := []string{"Usage:", "  tool   --flag    meaning", "````", "  other  --longer  detail"}; !reflect.DeepEqual(rows, want) {
		t.Errorf("got rows %q, want %q", rows, want)
	}
}
