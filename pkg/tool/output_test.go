package tool_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"crdx.org/oh/pkg/tool"
)

func TestLiveOutputKeepsABoundedTailAndReportsDroppedBytes(t *testing.T) {
	var output tool.OutputBuffer
	data := strings.Repeat("x", 300<<10) + "tail"
	count, err := output.Write([]byte(data))
	if err != nil || count != len(data) {
		t.Fatalf("write accepted %d bytes, %v", count, err)
	}
	if len(output.String()) != 256<<10 || !strings.HasSuffix(output.String(), "tail") || output.DroppedBytes() != len(data)-(256<<10) {
		t.Errorf("unexpected output tail: %d bytes, %d dropped", len(output.String()), output.DroppedBytes())
	}
	if !strings.Contains(output.Report(), "earlier output bytes") {
		t.Error("bounded output did not report omitted bytes")
	}
	if tool.LiveOutput(tool.WithOutput(t.Context(), &output)) != &output {
		t.Error("the execution context lost its output buffer")
	}
	if tool.LiveOutput(t.Context()) != nil {
		t.Error("an ordinary context offered a live output buffer")
	}
}

func TestTruncatedTextStartsAtACompleteUTF8Character(t *testing.T) {
	var output tool.OutputBuffer
	data := "€" + strings.Repeat("a", (256<<10)-2)
	if _, err := output.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(output.String()) || !utf8.ValidString(output.Report()) {
		t.Error("truncation introduced invalid UTF-8 into a valid stream")
	}
	if output.DroppedBytes() != len("€") || strings.Contains(output.String(), "€") || strings.Contains(output.String(), "�") {
		t.Errorf("truncation did not omit the whole character: %d dropped bytes", output.DroppedBytes())
	}
}
