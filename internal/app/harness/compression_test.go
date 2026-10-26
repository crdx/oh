package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ctl/analyse"
	"crdx.org/oh/internal/sim"
)

func TestGoldenProviderRequestCompressionRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	passes := map[string]func() string{}

	for _, test := range []struct {
		name         string
		model        string
		dialect      string
		wire         string
		path         string
		encoding     string
		shouldReject bool
	}{
		{"codex", "codex/fake", sim.Responses, "", "/responses", "zstd", false},
		{"anthropic", "anthropic/fake", sim.Messages, "", "/messages", "gzip", false},
		{"codex fallback", "codex/fake", sim.Responses, "", "/responses", "zstd", true},
		{"anthropic fallback", "anthropic/fake", sim.Messages, "", "/messages", "gzip", true},
		{"gateway completions", "opencode-go/fake", sim.Completions, sim.Completions, "/chat/completions", "", false},
		{"gateway responses", "opencode-go/fake", sim.Completions, sim.Responses, "/responses", "", false},
		{"gateway messages", "opencode-go/fake", sim.Completions, sim.Messages, "/messages", "", false},
		{"gateway generate content", "opencode-go/fake", sim.Completions, sim.GenerateContent, ":streamGenerateContent", "", false},
		{"ollama", "ollama/fake", sim.Completions, "", "/chat/completions", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := sim.New(&sim.Scenario{
				Model: "fake",
				Wire:  test.wire,
				Turns: []sim.Turn{{Say: "Compression works."}},
			})
			type capture struct {
				encoding string
				path     string
				header   http.Header
				status   int
				wire     []byte
				body     []byte
			}
			captures := make(chan capture, 4)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, test.path) {
					wire, err := io.ReadAll(request.Body)
					if err != nil {
						t.Error(err)
					}
					request.Body = io.NopCloser(bytes.NewReader(wire))
					body, err := sim.ReadBody(request)
					if err != nil {
						t.Error(err)
					}
					encoding := request.Header.Get("Content-Encoding")
					status := http.StatusOK
					if test.shouldReject && encoding != "" {
						status = http.StatusUnsupportedMediaType
					}
					captures <- capture{encoding, request.URL.Path, request.Header.Clone(), status, wire, body}
					if test.shouldReject && encoding != "" {
						writer.WriteHeader(http.StatusUnsupportedMediaType)
						_, _ = io.WriteString(writer, `{"error":{"message":"unsupported content encoding"}}`)
						return
					}
					request.Body = io.NopCloser(bytes.NewReader(wire))
				}
				endpoint.ServeHTTP(writer, request)
			}))
			t.Cleanup(server.Close)

			address := endpoint.Addresses(server.URL)[test.dialect]
			stateDirectory := t.TempDir()
			configDirectory := t.TempDir()
			if err := os.WriteFile(filepath.Join(configDirectory, "config.toml"), fmt.Appendf(nil, "version = %d\n[debug]\nwire = false\n", config.Format), 0o600); err != nil {
				t.Fatal(err)
			}
			environment := append(testBinaryEnvironment(t, stateDirectory), backend.EndpointVariable+"="+address, "OH_CONFIG_DIR="+configDirectory)
			workspace := reachableWorkspaceDir(t)
			output := runTestBinary(t, binary, workspace, environment,
				"-p", "--yolo", "-m", test.model, "reply briefly without using tools",
			)
			_, answerTail, isAnswerPresent := strings.Cut(output, "Compression works.")
			if !isAnswerPresent {
				t.Fatalf("the conversation did not complete: %s", output)
			}

			want := []string{test.encoding}
			if test.shouldReject {
				want = append(want, "")
			}
			var encodings []string
			var bodies [][]byte
			var report strings.Builder
			fmt.Fprintf(&report, "model: %s\n", test.model)
			for len(captures) > 0 {
				sent := <-captures
				encodings = append(encodings, sent.encoding)
				bodies = append(bodies, sent.body)
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(sent.body, &payload); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&report, "attempt %d: POST %s\n", len(bodies), sent.path)
				fmt.Fprintf(&report, "  content-encoding: %q\n  content-type: %s\n", sent.encoding, sent.header.Get("Content-Type"))
				fmt.Fprintf(&report, "  accept: %s\n  status: %d\n", sent.header.Get("Accept"), sent.status)
				fmt.Fprintf(&report, "  json fields: %s\n", strings.Join(slices.Sorted(maps.Keys(payload)), ", "))
				fmt.Fprintf(&report, "  upload smaller: %t\n  body equals first attempt: %t\n", len(sent.wire) < len(sent.body), bytes.Equal(bodies[0], sent.body))
				t.Logf("encoding %q: %d JSON bytes sent as %d wire bytes", sent.encoding, len(sent.body), len(sent.wire))
				if !json.Valid(sent.body) {
					t.Errorf("the decoded body is not JSON: %q", sent.body)
				}
				if sent.encoding != "" && len(sent.wire) >= len(sent.body) {
					t.Errorf("%s did not reduce %d bytes: %d", sent.encoding, len(sent.body), len(sent.wire))
				}
			}
			if !slices.Equal(encodings, want) {
				t.Errorf("got encodings %q, want %q", encodings, want)
			}
			if test.shouldReject && len(bodies) == 2 && !bytes.Equal(bodies[0], bodies[1]) {
				t.Error("fallback rewrote the conversation")
			}
			if len(endpoint.Requests()) != 1 {
				t.Errorf("the endpoint processed %d turns, want one", len(endpoint.Requests()))
			}
			analysisJSON := runTestBinary(t, binary, workspace, environment, "--ctl", "analyse", "--json")
			var analysis analyse.Analysis
			if err := json.Unmarshal([]byte(analysisJSON), &analysis); err != nil {
				t.Fatal(err)
			}
			wantRejections := 0
			if test.shouldReject {
				wantRejections = 1
				text := runTestBinary(t, binary, workspace, environment, "--ctl", "analyse")
				if !strings.Contains(text, "Compression Rejections") {
					t.Fatalf("the compression rejection is not visible in analyse: %s", text)
				}
			}
			if analysis.Faults.Total.CompressionRejections != wantRejections || len(analysis.Sessions) != 1 {
				t.Fatalf("unexpected stored rejection count: %s", analysisJSON)
			}
			wirePath := filepath.Join(stateDirectory, "org.crdx", "oh", "sessions", analysis.Sessions[0].Name, "debug", "wire.http.zst")
			if _, err := os.Stat(wirePath); !os.IsNotExist(err) {
				t.Fatalf("expected wire recording to be off: %v", err)
			}
			fmt.Fprintf(&report, "processed turns: %d\n", len(endpoint.Requests()))
			fmt.Fprintf(&report, "compression rejections in analyse: %d\nwire recording: disabled\n", analysis.Faults.Total.CompressionRejections)
			fmt.Fprintf(&report, "assistant: %s\n", scrubStartup(strings.TrimSpace("Compression works."+answerTail)))
			passes[test.name] = report.String
		})
	}
	compareWithGolden(t, "provider-request-compression", ".txt", passes)
}
