package analyse

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/util"
)

func writeJSON(analysis Analysis, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "    ")
	return encoder.Encode(analysis)
}

type reportRow struct {
	cells      []string
	appearance style.Style
}

type section struct {
	title       string
	header      []string
	textColumns int
	rows        []reportRow
	note        string
}

type presentation struct {
	currency     money.Currency
	isPerSession bool
}

const (
	totalName   = "Total"
	unknownCell = "—"
)

func writeText(analysis Analysis, report presentation, writer io.Writer) error {
	restoreStyle := style.Init(writer)
	defer restoreStyle()

	return drawText(analysis, report, writer)
}

func drawText(analysis Analysis, report presentation, writer io.Writer) error {
	sections := []section{
		cacheSection(analysis.PromptCache),
		contextSection(analysis.PromptCache),
		modelSection(analysis.Models, report.currency),
		activitySection(analysis.Activity),
		faultSection(analysis.Faults),
		toolSection(analysis.Tools),
	}
	if report.isPerSession {
		sections = append(sections, sessionSection(analysis.Sessions, report.currency))
	}

	return writeSections(writer, sections, analysis.SkippedSessions)
}

func writeSections(writer io.Writer, sections []section, skippedCount int) error {
	isFirstSection := true

	for _, part := range sections {
		if len(part.rows) == 0 {
			continue
		}
		if !isFirstSection {
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		isFirstSection = false

		if _, err := fmt.Fprintf(writer, "%s\n\n", style.MarkdownHeading(part.title)); err != nil {
			return err
		}
		if err := writeReportTable(writer, part, part.rows); err != nil {
			return err
		}
		if part.note != "" {
			if _, err := fmt.Fprintln(writer, style.Subtle(part.note)); err != nil {
				return err
			}
		}
	}

	if isFirstSection {
		if _, err := fmt.Fprintln(writer, style.Subtle("Nothing was recorded.")); err != nil {
			return err
		}
	}

	return writeSkipped(writer, skippedCount, isFirstSection)
}

func writeSkipped(writer io.Writer, skippedCount int, isReportEmpty bool) error {
	if skippedCount == 0 {
		return nil
	}

	if !isReportEmpty {
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(writer, style.Subtle(fmt.Sprintf(
		"%s %s in a legacy format and %s not considered.",
		util.FormatCount(skippedCount),
		pluralise(skippedCount, "session is", "sessions are"),
		pluralise(skippedCount, "was", "were"),
	)))

	return err
}

func cacheSection(analysis PromptCacheAnalysis) section {
	rows := make([]reportRow, 0, len(analysis.Providers)+1)
	for _, statistics := range analysis.Providers {
		rows = append(rows, cacheRow(model.ProviderName(statistics.Provider), statistics, style.Answer))
	}
	if len(analysis.Providers) > 1 {
		rows = append(rows, cacheRow(totalName, analysis.Total, style.Info))
	}

	return section{
		title:  "Prompt cache",
		header: []string{"Provider", "Sessions", "Requests", "Hits", "Misses", "Hit Rate", "Input Cached", "Tokens Read"},
		rows:   rows,
	}
}

func cacheRow(name string, statistics CacheStatistics, appearance style.Style) reportRow {
	return reportRow{
		appearance: appearance,
		cells: []string{
			name,
			util.FormatCount(statistics.Sessions),
			util.FormatCount(statistics.Requests),
			util.FormatCount(statistics.Hits),
			util.FormatCount(statistics.Misses),
			percentage(statistics.Hits, statistics.Requests),
			percentage(statistics.CachedTokens, statistics.InputTokens),
			util.FormatTokens(statistics.CachedTokens),
		},
	}
}

func contextSection(analysis PromptCacheAnalysis) section {
	rows := make([]reportRow, 0, len(analysis.Providers)+1)
	for _, statistics := range analysis.Providers {
		rows = append(rows, contextRow(model.ProviderName(statistics.Provider), statistics, style.Answer))
	}
	if len(analysis.Providers) > 1 {
		rows = append(rows, contextRow(totalName, analysis.Total, style.Info))
	}

	return section{
		title: "Tokens",
		header: []string{
			"Provider",
			"Average Input",
			"Peak Input",
			"Total Input",
			"Cache Reads",
			"Cache Writes",
			"Fresh Input",
			"Output",
		},
		rows: rows,
	}
}

func contextRow(name string, statistics CacheStatistics, appearance style.Style) reportRow {
	return reportRow{
		appearance: appearance,
		cells: []string{
			name,
			util.FormatTokens(statistics.AverageInputTokens()),
			util.FormatTokens(statistics.PeakInputTokens),
			util.FormatTokens(statistics.InputTokens),
			util.FormatTokens(statistics.CachedTokens),
			util.FormatTokens(statistics.WrittenTokens),
			util.FormatTokens(statistics.FreshTokens()),
			util.FormatTokens(statistics.OutputTokens),
		},
	}
}

func modelSection(analysis ModelAnalysis, currency money.Currency) section {
	rows := make([]reportRow, 0, len(analysis.Models)+1)
	for _, statistics := range analysis.Models {
		rows = append(rows, modelRow(statistics, currency, style.Answer))
	}
	if len(analysis.Models) > 1 {
		total := analysis.Total
		total.Provider = totalName
		total.Model = ""
		rows = append(rows, modelRow(total, currency, style.Info))
	}

	models := section{
		title:       "Models",
		textColumns: 2,
		header: []string{
			"Provider",
			"Model",
			"Sessions",
			"Requests",
			"Fresh Input",
			"Cache Reads",
			"Cache Writes",
			"Output",
			"Spend",
		},
		rows: rows,
	}
	if analysis.UnpricedModels > 0 {
		models.note = fmt.Sprintf(
			"%s %s no listed price, so %s spend is unknown and missing from the total.",
			util.FormatCount(analysis.UnpricedModels),
			pluralise(analysis.UnpricedModels, "model has", "models have"),
			pluralise(analysis.UnpricedModels, "its", "their"),
		)
	}

	return models
}

func modelRow(statistics ModelStatistics, currency money.Currency, appearance style.Style) reportRow {
	providerName := statistics.Provider
	if providerName != totalName {
		providerName = model.ProviderName(statistics.Provider)
	}

	return reportRow{
		appearance: appearance,
		cells: []string{
			providerName,
			statistics.Model,
			util.FormatCount(statistics.Sessions),
			util.FormatCount(statistics.Requests),
			util.FormatTokens(statistics.FreshTokens()),
			util.FormatTokens(statistics.CachedTokens),
			util.FormatTokens(statistics.WrittenTokens),
			util.FormatTokens(statistics.OutputTokens),
			formatSpend(statistics.Spend, statistics.IsPriced, currency),
		},
	}
}

func activitySection(analysis ActivityAnalysis) section {
	rows := make([]reportRow, 0, len(analysis.Providers)+1)
	for _, statistics := range analysis.Providers {
		rows = append(rows, activityRow(model.ProviderName(statistics.Provider), statistics, style.Answer))
	}
	if len(analysis.Providers) > 1 {
		rows = append(rows, activityRow(totalName, analysis.Total, style.Info))
	}

	return section{
		title: "Conversation",
		header: []string{
			"Provider",
			"Sessions",
			"Turns",
			"Prompts",
			"Replies",
			"Reasoning",
			"Tool Calls",
			"Average Turn",
			"Longest Turn",
			"Open For",
		},
		rows: rows,
	}
}

func activityRow(name string, statistics ActivityStatistics, appearance style.Style) reportRow {
	return reportRow{
		appearance: appearance,
		cells: []string{
			name,
			util.FormatCount(statistics.Sessions),
			util.FormatCount(statistics.Turns),
			util.FormatCount(statistics.Prompts),
			util.FormatCount(statistics.Replies),
			util.FormatCount(statistics.ReasoningBlocks),
			util.FormatCount(statistics.ToolCalls),
			formatTurn(statistics.AverageTurn(), statistics.Turns > 0),
			formatTurn(statistics.LongestTurn, statistics.Turns > 0),
			formatDuration(statistics.SessionTime),
		},
	}
}

func faultSection(analysis FaultAnalysis) section {
	if analysis.Total.IsQuiet() {
		return section{}
	}

	rows := make([]reportRow, 0, len(analysis.Providers)+1)
	for _, statistics := range analysis.Providers {
		if statistics.IsQuiet() {
			continue
		}
		rows = append(rows, faultRow(model.ProviderName(statistics.Provider), statistics, style.Answer))
	}
	if len(rows) > 1 {
		rows = append(rows, faultRow(totalName, analysis.Total, style.Info))
	}

	return section{
		title: "Faults",
		header: []string{
			"Provider",
			"Retries",
			"Interruptions",
			"Failures",
			"Silent Turns",
			"Rewrites",
			"Rebuilds",
			"Rebuilt Tokens",
		},
		rows: rows,
	}
}

func faultRow(name string, statistics FaultStatistics, appearance style.Style) reportRow {
	return reportRow{
		appearance: appearance,
		cells: []string{
			name,
			util.FormatCount(statistics.Retries),
			util.FormatCount(statistics.Interruptions),
			util.FormatCount(statistics.Failures),
			util.FormatCount(statistics.SilentTurns),
			util.FormatCount(statistics.PrefixRewrites),
			util.FormatCount(statistics.CacheRebuilds.Count()),
			util.FormatTokens(statistics.CacheRebuilds.WrittenTokens),
		},
	}
}

func toolSection(analysis ToolAnalysis) section {
	rows := make([]reportRow, 0, len(analysis.Tools)+1)
	for _, statistics := range analysis.Tools {
		rows = append(rows, toolRow(statistics.Name, statistics, style.Answer))
	}
	if len(analysis.Tools) > 1 {
		rows = append(rows, toolRow(totalName, analysis.Total, style.Info))
	}

	return section{
		title:  "Tools",
		header: []string{"Tool", "Calls", "Failures", "Cancelled", "Failure Rate", "Total Time", "Average Call"},
		rows:   rows,
	}
}

func toolRow(name string, statistics ToolStatistics, appearance style.Style) reportRow {
	return reportRow{
		appearance: appearance,
		cells: []string{
			name,
			util.FormatCount(statistics.Calls),
			util.FormatCount(statistics.Failures),
			util.FormatCount(statistics.Cancellations),
			percentage(statistics.Failures, statistics.Calls),
			formatDuration(statistics.Took),
			formatDuration(statistics.AverageCall()),
		},
	}
}

func sessionSection(sessions []SessionStatistics, currency money.Currency) section {
	rows := make([]reportRow, 0, len(sessions))
	for _, statistics := range sessions {
		rows = append(rows, sessionRow(statistics, currency))
	}

	return section{
		title:       "Sessions",
		textColumns: 2,
		header: []string{
			"Session",
			"Model",
			"Turns",
			"Requests",
			"Hit Rate",
			"Input",
			"Output",
			"Tool Calls",
			"Open For",
			"Spend",
		},
		rows: rows,
	}
}

func sessionRow(statistics SessionStatistics, currency money.Currency) reportRow {
	modelName := statistics.Model
	if modelName == "" {
		modelName = unknownCell
	}

	return reportRow{
		appearance: style.Answer,
		cells: []string{
			statistics.Name,
			modelName,
			util.FormatCount(statistics.Activity.Turns),
			util.FormatCount(statistics.Cache.Requests),
			percentage(statistics.Cache.Hits, statistics.Cache.Requests),
			util.FormatTokens(statistics.Cache.InputTokens),
			util.FormatTokens(statistics.Cache.OutputTokens),
			util.FormatCount(statistics.Activity.ToolCalls),
			formatDuration(statistics.Duration()),
			formatSpend(statistics.Spend, statistics.IsPriced, currency),
		},
	}
}

func reportTable(part section, rows []reportRow) *table.Table {
	columns := make([]table.Column, len(part.header))
	for index, title := range part.header {
		columns[index] = table.Column{Title: title}
		if index >= max(part.textColumns, 1) {
			columns[index].Align = table.Right
		}
	}

	cells := make([][]string, len(rows))
	for index, row := range rows {
		cells[index] = row.cells
	}

	return table.New(columns...).Fit(cells)
}

func writeReportTable(writer io.Writer, part section, rows []reportRow) error {
	reportedTable := reportTable(part, rows)

	if _, err := fmt.Fprintln(writer, style.Column(reportedTable.Header(0))); err != nil {
		return err
	}

	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, row.appearance(reportedTable.Row(row.cells, 0))); err != nil {
			return err
		}
	}

	return nil
}

func formatDuration(took time.Duration) string {
	if took <= 0 {
		return "0s"
	}

	return util.CompactDuration(took)
}

func formatTurn(took time.Duration, isTimed bool) string {
	if !isTimed {
		return unknownCell
	}

	return formatDuration(took)
}

func formatSpend(spend float64, isPriced bool, currency money.Currency) string {
	if !isPriced {
		return unknownCell
	}

	return currency.Format(spend)
}

func pluralise(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}

	return plural
}

func percentage[Count ~int | ~int64](part Count, whole Count) string {
	if whole <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(whole))
}
