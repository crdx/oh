package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/location"
)

func Show(ctx context.Context, output io.Writer, options Options) error {
	report := Collect(ctx, StoredSources(), time.Now)

	if options.JSON {
		document, err := json.Marshal(report)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintln(output, string(document))

		return err
	}

	_, err := io.WriteString(
		output, Render(report, time.Now(), TerminalGauges(os.Stdin, os.Stdout)),
	)

	return err
}

func StoredSources() []Source {
	usageSources := backend.UsageSources()
	sources := make([]Source, 0, len(usageSources))

	for _, source := range usageSources {
		sources = append(sources, Source{
			Provider:             source.Provider,
			Label:                source.Label,
			Reporter:             source.Reporter,
			CachePath:            location.GetUsageCachePath(source.Provider, false),
			HasIdleSessionWindow: source.HasIdleSessionWindow,
			IsSelfRefreshing:     source.Reporter != nil,
		})
	}

	return sources
}
