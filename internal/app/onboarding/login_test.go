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

	"crdx.org/io/internal/app/style"
	"crdx.org/io/pkg/provider/opencodego"
)

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
