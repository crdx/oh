package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"crdx.org/oh/internal/auth"
)

func TestConcurrentUpdatesAreSerialisedAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.Save(path, &auth.Credentials{Version: auth.Version}); err != nil {
		t.Fatal(err)
	}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- auth.Update(path, func(credentials *auth.Credentials) error {
			close(firstEntered)
			<-releaseFirst
			credentials.Anthropic = &auth.AnthropicCredentials{Access: "first"}
			return nil
		})
	}()
	<-firstEntered

	secondStarted := make(chan struct{})
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondDone <- auth.Update(path, func(credentials *auth.Credentials) error {
			close(secondEntered)
			if credentials.Anthropic == nil || credentials.Anthropic.Access != "first" {
				return errors.New("the second update did not see the first")
			}
			credentials.Codex = &auth.CodexCredentials{Access: "second"}
			return nil
		})
	}()
	<-secondStarted
	runtime.Gosched()

	select {
	case <-secondEntered:
		t.Fatal("the second update entered while the first held the credentials lock")
	default:
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Anthropic == nil || stored.Anthropic.Access != "first" || stored.Codex == nil || stored.Codex.Access != "second" {
		t.Errorf("concurrent updates lost credentials: %+v", stored)
	}
}

func TestSaveOpenCodeGoKeyPreservesCodexCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	credentials := &auth.Credentials{
		Codex: &auth.CodexCredentials{
			Access:    "access",
			Refresh:   "refresh",
			ExpiresAt: 123,
			AccountID: "account",
		},
	}
	if err := auth.Save(path, credentials); err != nil {
		t.Fatal(err)
	}

	if err := auth.SaveOpenCodeGoKey(path, "open-code-key"); err != nil {
		t.Fatal(err)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Codex == nil || stored.Codex.Access != "access" || stored.Codex.Refresh != "refresh" || stored.Codex.AccountID != "account" {
		t.Errorf("Codex credentials were replaced: %+v", stored.Codex)
	}
	if stored.OpenCodeGo == nil || stored.OpenCodeGo.APIKey != "open-code-key" {
		t.Errorf("got OpenCode credentials %+v", stored.OpenCodeGo)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("got permissions %o", got)
	}
}

func TestAnthropicCredentialsSitBesideTheOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.Save(path, &auth.Credentials{
		Codex: &auth.CodexCredentials{Access: "access", Refresh: "refresh", AccountID: "account"},
	}); err != nil {
		t.Fatal(err)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	stored.Anthropic = &auth.AnthropicCredentials{Access: "oat", Refresh: "refresh-oat", ExpiresAt: 7}
	if err := auth.Save(path, stored); err != nil {
		t.Fatal(err)
	}

	stored, err = auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Codex == nil || stored.Codex.Access != "access" || stored.Codex.AccountID != "account" {
		t.Errorf("Codex credentials were replaced: %+v", stored.Codex)
	}
	if stored.Anthropic == nil || stored.Anthropic.Access != "oat" || stored.Anthropic.Refresh != "refresh-oat" || stored.Anthropic.ExpiresAt != 7 {
		t.Errorf("got Anthropic credentials %+v", stored.Anthropic)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := auth.Load(path)
	if !errors.Is(err, auth.ErrUnsupportedVersion) {
		t.Fatalf("expected an unsupported format to be rejected, got %v", err)
	}
	if !strings.Contains(err.Error(), "login command") {
		t.Errorf("expected the error to say what to do about it, got %q", err)
	}
}

func TestCredentialsFromANewerOhAreRefusedBeforeTheirShapeIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"codex":"a-string-now"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := auth.Load(path)
	if !errors.Is(err, auth.ErrUnsupportedVersion) {
		t.Fatalf("expected the format to be named ahead of the shape, got %v", err)
	}
	if strings.Contains(err.Error(), "cannot unmarshal") {
		t.Errorf("expected the decoder complaint to be replaced by the format, got %v", err)
	}
}

func TestSavingReplacesCredentialsInAnOlderFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	flat := `{"access":"old","refresh":"refresh-me","account_id":"account","expires_at":1}`
	if err := os.WriteFile(path, []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := auth.SaveOpenCodeGoKey(path, "key"); err != nil {
		t.Fatalf("expected logging in again to write over the older format, got %v", err)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OpenCodeGo == nil || stored.OpenCodeGo.APIKey != "key" {
		t.Errorf("got OpenCode credentials %+v", stored.OpenCodeGo)
	}
}

func TestSaveOpenCodeGoKeyCreatesCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "auth.json")
	if err := auth.SaveOpenCodeGoKey(path, "key"); err != nil {
		t.Fatal(err)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OpenCodeGo == nil || stored.OpenCodeGo.APIKey != "key" {
		t.Errorf("got OpenCode credentials %+v", stored.OpenCodeGo)
	}
}
