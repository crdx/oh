package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
)

func FuzzABarIsBuiltAndDrawnWithoutFallingOver(fuzzer *testing.F) {
	for _, seed := range []string{
		"[bar.top]\nleft = [{ segment = \"activity-spinner\", idle = \"·\", frames = [\"·\"], rate = \"1s\" }]\n",
		"[bar.top]\ncenter = [{ segment = \"local-time\", format = \"15:04\" }]\n",
		"[bar.top]\nright = [{ segment = \"scroll-overflow\", direction = \"up\" }]\n",
		"[bar.bottom]\nleft = [{ segment = \"workspace-dir\", type = \"base\" }]\n",
		"[bar.bottom]\nleft = [{ segment = \"path-grants\", type = \"full\" }]\n",
		"[bar.bottom]\nright = [{ segment = \"session-name\", emoji = true }]\n",
		"[bar.bottom]\ncenter = [{ segment = \"subscription-usage\", rate = \"1m\" }]\n",
		"[bar.top]\nleft = [{ segment = \"git-branch\", rate = \"1s\" }]\n",
		"[bar.top]\nleft = [{ segment = \"cache-usage\" }, { segment = \"context-usage\" }]\n",
		"[bar.top]\nleft = [{ segment = \"jobs\" }, { segment = \"exposed-ports\" }]\n",
		"[bar.top]\nleft = [{ segment = \"turn-timer\" }, { segment = \"turn-count\" }]\n",
		"[bar.top]\nleft = [{ segment = \"session-spend\" }, { segment = \"active-model\" }]\n",
		"[bar.top]\nleft = [{ segment = \"fast-mode\" }, { segment = \"mode-toggle\" }]\n",
		"[bar.top]\nleft = [{ segment = \"session-emoji\" }]\n",
		"[ui.theme]\nnormal = \"#010203 bold\"\n",
	} {
		fuzzer.Add(seed, byte(80))
	}

	directory := fuzzer.TempDir()
	registry := testRegistry()

	fuzzer.Fuzz(func(t *testing.T, body string, cells byte) {
		if len(body) > 4096 {
			t.Skip("longer than a config is ever written")
		}

		path := filepath.Join(directory, "config.toml")
		written := fmt.Sprintf("version = %d\n", config.Format) + body
		if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
			t.Fatal(err)
		}

		settings, err := config.Load(path)
		if err != nil {
			return
		}

		restoreTheme := style.ApplyTheme(settings.Ui.Theme)
		defer restoreTheme()

		layout, err := settings.BuildLayout(registry)
		if err != nil {
			return
		}

		for _, position := range segment.Positions {
			context := segment.Context{HiddenLinesAbove: 3, HiddenLinesBelow: 0}
			_ = bar.RenderWithin(layout, position, context, int(cells))
			_ = bar.RenderWithin(layout, position, context, 0)
		}

		_ = layout.NextRefresh(segment.Phase{At: time.Now(), IsRunning: true})
	})
}
