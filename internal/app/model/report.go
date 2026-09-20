package model

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/table"
)

const (
	providerColumn   = 12
	listedColumn     = 6
	selectableColumn = 10
	ignoredColumn    = 7
)

const nothingRecordedMark = "—"

type providerReport struct {
	Provider        string
	Source          string
	Why             string
	ListedCount     int
	SelectableCount int
	IgnoredModels   []ignoredModel
	ChangedModels   []modelChange
	IsRecorded      bool
}

func (self providerReport) hasRecorded() bool {
	return self.IsRecorded
}

func updateTable() *table.Table {
	return table.New(
		table.Column{Title: "Provider", Width: providerColumn},
		table.Column{Title: "Models", Width: listedColumn, Align: table.Right},
		table.Column{Title: "Selectable", Width: selectableColumn, Align: table.Right},
		table.Column{Title: "Ignored", Width: ignoredColumn, Align: table.Right},
		table.Column{Title: "Source"},
	)
}

func ignoredTable(rows [][]string) *table.Table {
	return table.New(
		table.Column{Title: "Provider"},
		table.Column{Title: "Model"},
		table.Column{Title: "Reason", Style: style.Subtle},
	).Fit(rows)
}

func changeTable(rows [][]string) *table.Table {
	return table.New(
		table.Column{Title: "Provider"},
		table.Column{Title: "Model"},
		table.Column{Title: "Change"},
	).Fit(rows)
}

func writeLine(output io.Writer, line string) {
	_, _ = fmt.Fprintln(output, strings.TrimRight(line, " "))
}

func writeHeadings(output io.Writer) {
	writeLine(output, style.Column(updateTable().Header(0)))
}

func writeProviderReport(output io.Writer, report providerReport) {
	writeLine(output, updateTable().Row(providerCells(report), 0))
}

func ignoredRows(reports []providerReport) [][]string {
	var rows [][]string

	for _, report := range reports {
		for _, model := range report.IgnoredModels {
			rows = append(rows, []string{ProviderName(report.Provider), model.Name, model.Reason})
		}
	}

	return rows
}

func writeIgnoredModels(output io.Writer, reports []providerReport) {
	rows := ignoredRows(reports)
	if len(rows) == 0 {
		return
	}

	ignoredModelsTable := ignoredTable(rows)

	_, _ = fmt.Fprintln(output)
	writeLine(output, style.Column(ignoredModelsTable.Header(0)))

	for _, row := range rows {
		writeLine(output, ignoredModelsTable.Row(row, 0))
	}
}

func changeRows(reports []providerReport) [][]string {
	var rows [][]string

	for _, report := range reports {
		for _, change := range report.ChangedModels {
			rows = append(rows, []string{
				ProviderName(report.Provider),
				change.Name,
				changeCell(change.Change),
			})
		}
	}

	return rows
}

func changeCell(change string) string {
	if change == addedChange {
		return style.InsertedText(change)
	}

	return style.DeletedText(change)
}

func writeChangedModels(output io.Writer, reports []providerReport) {
	rows := changeRows(reports)
	if len(rows) == 0 {
		return
	}

	changedModelsTable := changeTable(rows)

	_, _ = fmt.Fprintln(output)
	writeLine(output, style.Column(changedModelsTable.Header(0)))

	for _, row := range rows {
		writeLine(output, changedModelsTable.Row(row, 0))
	}
}

func providerCells(report providerReport) []string {
	if !report.hasRecorded() {
		return []string{
			ProviderName(report.Provider),
			style.Subtle(nothingRecordedMark), "", "",
			style.Subtle(report.Why),
		}
	}

	return []string{
		ProviderName(report.Provider),
		strconv.Itoa(report.ListedCount),
		selectableCount(report.SelectableCount),
		ignoredCount(len(report.IgnoredModels)),
		sourceCell(report),
	}
}

func sourceCell(report providerReport) string {
	source := style.Info(report.Source)
	if report.Why == "" {
		return source
	}

	return source + " " + style.Subtle("("+report.Why+")")
}

func selectableCount(count int) string {
	if count == 0 {
		return style.Subtle(count)
	}

	return style.Success(count)
}

func ignoredCount(count int) string {
	if count == 0 {
		return ""
	}

	return style.Change(count)
}

const ignoredHintCommand = "oh -u -I"

func writeIgnoredHint(output io.Writer, reports []providerReport) {
	var count int
	for _, report := range reports {
		count += len(report.IgnoredModels)
	}

	if count == 0 {
		return
	}

	_, _ = fmt.Fprintln(output, style.Subtle("Run ")+
		style.Accent(ignoredHintCommand)+
		style.Subtle(" to see "+ignoredSubject(count)+"."))
}

func ignoredSubject(count int) string {
	if count == 1 {
		return "the model that was ignored"
	}

	return fmt.Sprintf("the %d models that were ignored", count)
}
