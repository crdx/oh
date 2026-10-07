package onboarding

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/auth"
	"crdx.org/oh/pkg/provider/anthropic"
	"crdx.org/oh/pkg/provider/codex"
)

const (
	terminalDeadline = 10 * time.Second
	pressEnd         = "\x1b[F"
	pressEnter       = "\r"
)

var presentedState = regexp.MustCompile(`[?&]state=([0-9a-f]+)`)

func TestAFirstRunOverATerminalCanHandOverToTheSimulation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv(location.StateDirVariable, "")

	settings := storedSettings(t)
	controller, terminal := ptytest.OpenSized(t, 100, 30)
	screen := ptytest.Record(controller)

	type prepared struct {
		isSimulated bool
		err         error
	}
	result := make(chan prepared, 1)
	go func() {
		isSimulated, err := PrepareConfig(Options{Input: terminal, Output: terminal, Settings: settings})
		result <- prepared{isSimulated: isSimulated, err: err}
	}()

	if !screen.WaitForCount(greeting[:4], 1, terminalDeadline) {
		t.Fatalf("the opening was never typed, having drawn %q", screen.String())
	}
	typeInto(t, controller, pressEnter)
	if !screen.WaitForCount(providerPrompt, 1, terminalDeadline) {
		t.Fatalf("no provider was asked for, having drawn %q", screen.String())
	}
	typeInto(t, controller, pressEnd+pressEnter)

	select {
	case got := <-result:
		if got.err != nil || !got.isSimulated {
			t.Errorf("got simulated=%t, %v; want the simulation chosen", got.isSimulated, got.err)
		}
	case <-time.After(terminalDeadline):
		t.Fatalf("choosing the simulation finished nothing, having drawn %q", screen.String())
	}
	if !screen.WaitForCount(farewell, 1, terminalDeadline) {
		t.Errorf("the farewell was not drawn, in %q", screen.String())
	}
}

func TestASignInOverATerminalTakesThePastedRedirect(t *testing.T) {
	for _, providerName := range []string{model.CodexProvider, model.AnthropicProvider} {
		t.Run(providerName, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("PATH", t.TempDir())

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(writer, `{"access_token":%q,"refresh_token":"next","expires_in":3600}`, signedInAccess)
			}))
			t.Cleanup(server.Close)
			codex.TokenURL, anthropic.TokenURL = server.URL, server.URL
			t.Cleanup(func() { codex.TokenURL, anthropic.TokenURL = "", "" })

			controller, terminal := ptytest.OpenSized(t, 400, 30)
			screen := ptytest.Record(controller)

			result := make(chan error, 1)
			go func() { result <- Login(providerName, terminal, terminal) }()

			if !screen.WaitForCount(pasteHint, 1, terminalDeadline) {
				t.Fatalf("nothing asked for the redirect, having drawn %q", screen.String())
			}
			state := presentedState.FindStringSubmatch(screen.String())
			if state == nil {
				t.Fatalf("no state was presented in %q", screen.String())
			}
			typeInto(t, controller, "http://localhost/callback?code=granted&state="+state[1]+"\n")

			select {
			case err := <-result:
				if err != nil {
					t.Fatalf("signing in failed: %v, having drawn %q", err, screen.String())
				}
			case <-time.After(terminalDeadline):
				t.Fatalf("the pasted redirect was never taken, having drawn %q", screen.String())
			}

			stored, err := auth.Load(auth.Path())
			if err != nil {
				t.Fatal(err)
			}
			isStored := map[string]bool{
				model.CodexProvider:     stored.Codex != nil && stored.Codex.Access == signedInAccess,
				model.AnthropicProvider: stored.Anthropic != nil && stored.Anthropic.Access == signedInAccess,
			}
			if !isStored[providerName] {
				t.Errorf("the access token was not stored for %s: %+v", providerName, stored)
			}
		})
	}
}

func TestAnUnknownProviderCannotBeSignedIntoOrOutOf(t *testing.T) {
	unknown := provider{identifier: "somewhere"}

	if err := login(nil, nil)(unknown, func(string) {}); err == nil {
		t.Error("signing into an unknown provider succeeded")
	}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := removeCredentials(unknown); err == nil {
		t.Error("signing out of an unknown provider succeeded")
	}
}

const signedInAccess = "header.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjb3VudCJ9fQ.signature"

func typeInto(t *testing.T, controller *os.File, text string) {
	t.Helper()

	if _, err := controller.WriteString(text); err != nil {
		t.Fatal(err)
	}
}
