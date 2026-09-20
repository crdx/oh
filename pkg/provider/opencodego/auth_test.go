package opencodego_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/auth"
	"crdx.org/io/pkg/provider/opencodego"
)

func TestStoredKeyReadsOpenCodeGoCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.SaveOpenCodeGoKey(path, "stored-key"); err != nil {
		t.Fatal(err)
	}

	key, err := opencodego.StoredKeyAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if key != "stored-key" {
		t.Errorf("got key %q", key)
	}
}

func TestStoredKeyReportsMissingOpenCodeGoCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.Save(path, &auth.Credentials{Codex: &auth.CodexCredentials{Access: "codex-access"}}); err != nil {
		t.Fatal(err)
	}

	_, err := opencodego.StoredKeyAt(path)
	if err == nil || !strings.Contains(err.Error(), "login command with opencode-go") {
		t.Fatalf("got error %v", err)
	}
}

func TestStoredKeyReportsAMissingCredentialFileAsLoggedOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")

	_, err := opencodego.StoredKeyAt(path)
	if err == nil || !strings.Contains(err.Error(), "login command with opencode-go") {
		t.Fatalf("got error %v", err)
	}
}

func TestStoredKeyPreservesCredentialFileFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := opencodego.StoredKeyAt(path)
	if err == nil || !strings.Contains(err.Error(), "parse credentials") {
		t.Fatalf("got error %v", err)
	}
	if strings.Contains(err.Error(), "login command") {
		t.Errorf("credential failure was replaced with a login instruction: %v", err)
	}
}

func TestDefaultKeyHelpersUseTheSharedCredentialStore(t *testing.T) {
	stateDirectory := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDirectory)

	path := opencodego.CredentialsPath()
	wantPath := filepath.Join(stateDirectory, "org.crdx", "io", "auth.json")
	if path != wantPath {
		t.Fatalf("got credentials path %q, want %q", path, wantPath)
	}

	if err := auth.Save(path, &auth.Credentials{
		Codex: &auth.CodexCredentials{Access: "keep-me"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := opencodego.SaveKey("open-code-key"); err != nil {
		t.Fatal(err)
	}

	key, err := opencodego.StoredKey()
	if err != nil {
		t.Fatal(err)
	}
	if key != "open-code-key" {
		t.Errorf("got key %q", key)
	}

	credentials, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Codex == nil || credentials.Codex.Access != "keep-me" {
		t.Errorf("saving the OpenCode Go key replaced other credentials: %+v", credentials)
	}
}

func TestSaveKeyAtWritesAStoreThatStoredKeyAtReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := opencodego.SaveKeyAt(path, "round-trip-key"); err != nil {
		t.Fatal(err)
	}

	key, err := opencodego.StoredKeyAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if key != "round-trip-key" {
		t.Errorf("got key %q", key)
	}
}
