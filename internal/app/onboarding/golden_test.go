package onboarding

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/oauth"
)

const authorisationURL = "https://example.test/authorise"

func TestGoldenAuthenticationFlowsMatchTheGoldens(t *testing.T) {
	tests := map[string]func(*testing.T, *bytes.Buffer) error{
		"login-picker": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				choose: menuChoices(output, 0),
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return nil
				},
			}
			_, err := harry.chooseProvider("")
			return err
		},
		"login-anthropic": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				login: func(chosen provider, presentAddress func(string)) error {
					if chosen.identifier != model.AnthropicProvider {
						t.Errorf("got provider %q", chosen.identifier)
					}
					presentAddress("https://example.test/authorise")
					return nil
				},
			}
			_, err := harry.chooseProvider(model.AnthropicProvider)
			return err
		},
		"login-browser-failure": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise?token=one")
					return nil
				},
				openBrowser: func(string) error { return errors.New("no browser is available") },
			}
			_, err := harry.chooseProvider(model.CodexProvider)
			return err
		},
		"login-long-url": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			address := "https://auth.example.test/oauth/authorize?client_id=oh-desktop&code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ&code_challenge_method=S256&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&response_type=code&scope=openid%20profile%20email%20offline_access"
			var openedAddress string
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress(address)
					return nil
				},
				openBrowser: func(got string) error {
					if !strings.Contains(link.Plain(output.String()), address) {
						t.Error("browser was opened before the complete address was printed")
					}
					openedAddress = got
					return nil
				},
			}
			if _, err := harry.chooseProvider(model.CodexProvider); err != nil {
				return err
			}
			if openedAddress != address {
				t.Errorf("opened %q", openedAddress)
			}
			return nil
		},
		"login-direct-failure": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return errors.New("authorisation was refused")
				},
			}
			_, err := harry.chooseProvider(model.AnthropicProvider)
			if err == nil {
				return errors.New("expected direct login to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"login-pasted-redirect": func(_ *testing.T, output *bytes.Buffer) error {
			redirect := "http://localhost:1455/auth/callback?code=accepted&state=expected"
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					_, _ = fmt.Fprintln(output, redirect)
					_, err := oauth.CodeFromRedirect(redirect, "expected")
					return err
				},
			}
			_, err := harry.chooseProvider(model.CodexProvider)
			return err
		},
		"login-malformed-redirect": func(_ *testing.T, output *bytes.Buffer) error {
			redirect := "not a complete URL"
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					_, _ = fmt.Fprintln(output, redirect)
					_, err := oauth.CodeFromRedirect(redirect, "expected")
					return err
				},
			}
			_, err := harry.chooseProvider(model.CodexProvider)
			if err == nil {
				return errors.New("expected malformed redirect to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"login-redirect-state-mismatch": func(_ *testing.T, output *bytes.Buffer) error {
			redirect := "http://localhost:1455/auth/callback?code=accepted&state=wrong"
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					_, _ = fmt.Fprintln(output, redirect)
					_, err := oauth.CodeFromRedirect(redirect, "expected")
					return err
				},
			}
			_, err := harry.chooseProvider(model.CodexProvider)
			if err == nil {
				return errors.New("expected mismatched redirect to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"login-opencode-go": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				login: func(chosen provider, _ func(string)) error {
					if chosen.identifier != model.OpencodeGoProvider {
						t.Errorf("got provider %q", chosen.identifier)
					}
					return storeOpenCodeGoKey(
						typed("secret\n", output),
						output,
						filepath.Join(t.TempDir(), "auth.json"),
						func(string) error { return nil },
					)
				},
			}
			_, err := harry.chooseProvider(model.OpencodeGoProvider)
			return err
		},
		"login-opencode-rejected": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				choose: menuChoices(output, 2, 0),
				login: func(chosen provider, presentAddress func(string)) error {
					if chosen.identifier == model.OpencodeGoProvider {
						return storeOpenCodeGoKey(
							typed("bad-key\n", output),
							output,
							filepath.Join(t.TempDir(), "auth.json"),
							func(string) error { return errors.New("the key was refused") },
						)
					}
					presentAddress("https://example.test/authorise")
					return nil
				},
			}
			_, err := harry.chooseProvider("")
			return err
		},
		"login-retry": func(_ *testing.T, output *bytes.Buffer) error {
			attempts := 0
			harry := wizard{
				output: output,
				choose: menuChoices(output, 0, 1),
				login: func(_ provider, presentAddress func(string)) error {
					attempts++
					presentAddress("https://example.test/authorise")
					if attempts == 1 {
						return errors.New("authorisation was refused")
					}
					return nil
				},
			}
			_, err := harry.chooseProvider("")
			return err
		},
		"login-manage-sign-out": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				choose: menuChoices(output, 0, 1),
				login: func(provider, func(string)) error {
					t.Error("sign-in was started")
					return nil
				},
				logout:                func(provider) error { return nil },
				isLoggedIn:            func(string) bool { return true },
				isManagingCredentials: true,
			}
			_, err := harry.chooseProvider("")
			return err
		},
		"login-manage-remove-key": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				choose: menuChoices(output, 2, 1),
				login: func(provider, func(string)) error {
					t.Error("a key was asked for")
					return nil
				},
				logout:                func(provider) error { return nil },
				isLoggedIn:            func(string) bool { return true },
				isManagingCredentials: true,
			}
			_, err := harry.chooseProvider("")
			return err
		},
		"login-manage-cancelled": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			answers := []error{nil, ErrCancelled, nil}
			choices := []int{0, 0, 2}
			answerIndex := 0
			harry := wizard{
				output: output,
				choose: func(prompt string, labels []string) (int, error) {
					chosen, err := choices[answerIndex], answers[answerIndex]
					answerIndex++
					if _, writeErr := output.WriteString(menu.RenderMenu(prompt, labels, chosen)); writeErr != nil {
						return 0, writeErr
					}
					return chosen, err
				},
				login: func(chosen provider, _ func(string)) error {
					if chosen.identifier != model.OpencodeGoProvider {
						t.Errorf("got provider %q", chosen.identifier)
					}
					return nil
				},
				logout: func(provider) error {
					t.Error("a provider was signed out of")
					return nil
				},
				isLoggedIn: func(providerName string) bool {
					return providerName == model.CodexProvider
				},
				isManagingCredentials: true,
			}
			_, err := harry.chooseProvider("")
			return err
		},
		"login-cancelled": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				choose: func(prompt string, labels []string) (int, error) {
					if _, err := output.WriteString(menu.RenderMenu(prompt, labels, 0)); err != nil {
						return 0, err
					}
					return 0, ErrCancelled
				},
			}
			_, err := harry.chooseProvider("")
			if !errors.Is(err, ErrCancelled) {
				return fmt.Errorf("got %w", err)
			}
			return nil
		},
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			restoreStyle := style.Init(&output)
			t.Cleanup(restoreStyle)

			if err := run(t, &output); err != nil {
				t.Fatal(err)
			}
			assertScreenGolden(t, name, output.String())
			if name == "login-anthropic" || name == "login-browser-failure" || name == "login-long-url" {
				assertANSIGolden(t, name, output.String())
			}
		})
	}
}

func TestGoldenThePaintedWizardMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer

	harry := wizard{
		output:        &output,
		choose:        menuChoices(&output, 0, 1),
		login:         func(_ provider, presentAddress func(string)) error { presentAddress(authorisationURL); return nil },
		refreshModels: func() error { return nil },
		getModels: func() []model.Choice {
			return []model.Choice{
				{Provider: model.CodexProvider, ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"medium"}},
				{Provider: model.CodexProvider, ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", EffortLevels: []string{"high"}},
			}
		},
		setInitialModel: func(string) error { return nil },
	}

	if err := harry.castSpell(); err != nil {
		t.Fatal(err)
	}
	assertANSIGolden(t, "first-run-painted", output.String())
}

func TestGoldenThePaintedSignOutMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer

	harry := wizard{
		output: &output,
		choose: menuChoices(&output, 0, 1),
		login: func(provider, func(string)) error {
			t.Error("sign-in was started")
			return nil
		},
		logout:                func(provider) error { return nil },
		isLoggedIn:            func(string) bool { return true },
		isManagingCredentials: true,
	}

	if _, err := harry.chooseProvider(""); err != nil {
		t.Fatal(err)
	}
	assertANSIGolden(t, "login-manage-sign-out-painted", output.String())
}

func TestGoldenThePaintedSimulationRowMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer

	harry := wizard{
		isSimulationOffered: true,
		output:              &output,
		choose:              menuChoices(&output, len(providers)),
		login: func(provider, func(string)) error {
			t.Error("the simulation asked to sign in")
			return nil
		},
	}

	if err := harry.castSpell(); err != nil {
		t.Fatal(err)
	}
	assertANSIGolden(t, "first-run-simulation-painted", output.String())
}

func TestGoldenThePaintedWizardShowsWhatWentWrongMatchingTheGolden(t *testing.T) {
	var output bytes.Buffer

	harry := wizard{
		output: &output,
		choose: menuChoices(&output, 2, 0),
		login: func(chosen provider, presentAddress func(string)) error {
			if chosen.identifier == model.OpencodeGoProvider {
				return storeOpenCodeGoKey(
					typed("bad-key\n", &output),
					&output,
					filepath.Join(t.TempDir(), "auth.json"),
					func(string) error { return errors.New("the key was refused") },
				)
			}
			presentAddress(authorisationURL)
			return nil
		},
		openBrowser: func(string) error { return errors.New("no browser is available") },
	}

	if _, err := harry.chooseProvider(""); err != nil {
		t.Fatal(err)
	}
	assertANSIGolden(t, "login-failure-painted", output.String())
}

func TestGoldenOpenCodeGoOnboardingMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer
	restoreStyle := style.Init(&output)
	t.Cleanup(restoreStyle)

	harry := wizard{
		output: &output,
		choose: menuChoices(&output, 2, 0),
		login: func(chosen provider, _ func(string)) error {
			if chosen.identifier != model.OpencodeGoProvider {
				t.Errorf("got provider %q", chosen.identifier)
			}
			return storeOpenCodeGoKey(
				typed("secret\n", &output),
				&output,
				filepath.Join(t.TempDir(), "auth.json"),
				func(string) error { return nil },
			)
		},
		refreshModels: func() error { return nil },
		getModels: func() []model.Choice {
			return []model.Choice{{
				Provider:     model.OpencodeGoProvider,
				ID:           "deepseek-v4-pro",
				Name:         "DeepSeek V4 Pro",
				EffortLevels: []string{"medium", "high"},
			}}
		},
		setInitialModel: func(selection string) error {
			if selection != "opencode-go/deepseek-v4-pro@high" {
				t.Errorf("saved %q", selection)
			}
			return nil
		},
	}

	if err := harry.castSpell(); err != nil {
		t.Fatal(err)
	}
	assertScreenGolden(t, "first-run-opencode-go", output.String())
}

func typed(text string, terminal io.Writer) io.Reader {
	return io.TeeReader(strings.NewReader(text), terminal)
}

func menuChoices(output *bytes.Buffer, choices ...int) func(string, []string) (int, error) {
	choiceIndex := 0
	return func(prompt string, labels []string) (int, error) {
		chosen := choices[choiceIndex]
		choiceIndex++
		_, err := output.WriteString(menu.RenderMenu(prompt, labels, chosen))
		return chosen, err
	}
}

func assertANSIGolden(t *testing.T, name string, rendered string) {
	t.Helper()

	got := strings.NewReplacer("\x1b", `\e`, "\r", `\r`).Replace(rendered)
	path := filepath.Join("testdata", name+".ansi")
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // the test's own golden
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("rendering differs from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

func assertScreenGolden(t *testing.T, name string, rendered string) {
	t.Helper()

	got := visibleTranscript(rendered)
	path := filepath.Join("testdata", name+".screen")
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // the test's own golden
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("rendering differs from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}
