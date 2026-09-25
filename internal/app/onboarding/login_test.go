package onboarding

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/auth"
	"crdx.org/oh/pkg/provider/opencodego"
)

func TestRemovingCredentialsPreservesTheOtherProviders(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := auth.Path()
	if err := auth.Save(path, &auth.Credentials{
		Codex:      &auth.CodexCredentials{Access: "codex"},
		Anthropic:  &auth.AnthropicCredentials{Access: "anthropic"},
		OpenCodeGo: &auth.OpenCodeGoCredentials{APIKey: "opencode"},
	}); err != nil {
		t.Fatal(err)
	}

	chosen, found := providerNamed(model.OpencodeGoProvider)
	if !found {
		t.Fatal("OpenCode Go provider is missing")
	}
	if err := removeCredentials(chosen); err != nil {
		t.Fatal(err)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OpenCodeGo != nil {
		t.Errorf("OpenCode Go credentials remain: %+v", stored.OpenCodeGo)
	}
	if stored.Codex == nil || stored.Codex.Access != "codex" || stored.Anthropic == nil || stored.Anthropic.Access != "anthropic" {
		t.Errorf("another provider's credentials changed: %+v", stored)
	}
}

func TestTheKeyPromptLeavesTheEchoToTheTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	var output bytes.Buffer
	if err := storeOpenCodeGoKey(strings.NewReader("  pasted-key  \n"), &output, path, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(output.String()); got != openCodeGoPrompt {
		t.Errorf("got prompt %q, and anything beyond it is an echo of the key", got)
	}
	key, err := opencodego.StoredKeyAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if key != "pasted-key" {
		t.Errorf("got key %q", key)
	}
}

func TestLoginOpenCodeValidatesBeforeStoringTheKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	validationError := errors.New("the key was refused")

	err := storeOpenCodeGoKey(
		strings.NewReader("bad-key\n"),
		&bytes.Buffer{},
		path,
		func(key string) error {
			if key != "bad-key" {
				t.Errorf("validated %q", key)
			}
			return validationError
		},
	)
	if !errors.Is(err, validationError) {
		t.Fatalf("got error %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rejected key was stored: %v", err)
	}
}

func TestOpenCodeKeyValidationReadsAccountUsageWithTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/usage" {
			t.Errorf("requested %s", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer accepted-key" {
			t.Errorf("got authorisation %q", got)
		}
		_, _ = writer.Write([]byte(`{"usage":{}}`))
	}))
	t.Cleanup(server.Close)

	if err := validateOpenCodeGoKeyAt("accepted-key", server.URL+"/usage"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeKeyValidationReportsARejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "invalid API key", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	err := validateOpenCodeGoKeyAt("bad-key", server.URL+"/usage")
	if err == nil {
		t.Fatal("expected the rejected key to fail validation")
	}
}

func TestLoginOpenCodeRejectsAnEmptyKey(t *testing.T) {
	err := storeOpenCodeGoKey(
		strings.NewReader(" \n"),
		&bytes.Buffer{},
		filepath.Join(t.TempDir(), "auth.json"),
		func(string) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("got error %v", err)
	}
}
