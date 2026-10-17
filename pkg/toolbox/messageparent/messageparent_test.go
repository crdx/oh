package messageparent_test

import (
	"context"
	"errors"
	"testing"

	"crdx.org/oh/pkg/toolbox/messageparent"
)

func TestAMessageReachesTheParentTrimmed(t *testing.T) {
	var received []string
	parentTool := messageparent.New(func(_ context.Context, message string) error {
		received = append(received, message)
		return nil
	})
	call, err := parentTool.Parse(`{"message":"  halfway there \n"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rendering := call.Rendering(); rendering.Subject != "halfway there" {
		t.Errorf("drew %q", rendering.Subject)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != messageparent.Result || len(received) != 1 || received[0] != "halfway there" {
		t.Errorf("said %q and sent %q", result.Output, received)
	}
}

func TestABlankMessageIsRefusedBeforeItIsSent(t *testing.T) {
	parentTool := messageparent.New(func(context.Context, string) error {
		t.Error("a blank message was sent")
		return nil
	})
	if _, err := parentTool.Parse(`{"message":" \n "}`); err == nil {
		t.Error("a blank message was accepted")
	}
}

func TestARefusalReachesTheChild(t *testing.T) {
	refusal := errors.New("you are being stopped")
	call, err := messageparent.New(func(context.Context, string) error { return refusal }).Parse(`{"message":"late"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); !errors.Is(err, refusal) {
		t.Errorf("came back %v", err)
	}
}
