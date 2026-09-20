package call

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"crdx.org/io/internal/app/dynamic"
	"crdx.org/io/internal/app/link"
	"crdx.org/io/internal/app/markdown"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/width"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/tool"
)

const (
	durationWidth    = 6
	bytesPerMegabyte = 1 << 20

	noTimeAtAll = "0s"
)

type Label struct {
	Name            string
	Subject         string
	Emphasis        tool.Emphasis
	Qualifier       string
	ReadOnly        bool
	NameStyle       style.Style
	Accent          string
	AccentStyle     style.Style
	ResultURI       string
	TimeLimit       time.Duration
	PathRoots       link.Roots
	Continuation    []Label
	lineRange       string
	renderedSubject string
}

func (self Label) Elide(room int) dynamic.Label {
	return self.elide(room)
}

func (self Label) Render() string {
	var name string
	if self.Name != "" {
		name = self.style()(self.Name)
		if self.ResultURI != "" {
			name = link.RenderURL(name, self.ResultURI)
		}
	}
	line := name
	isQualifierLinked := false

	if self.Subject != "" {
		subject := self.renderSubject()
		if firstLine := lineRangeStart(self.lineRange); firstLine != "" {
			if self.Qualifier != "" {
				subject += " " + self.renderQualifier()
				isQualifierLinked = true
			}
			subject = link.RenderPathAtLine(subject, self.Subject, self.PathRoots, firstLine)
		}
		line += " " + subject
	}

	if self.Qualifier != "" && !isQualifierLinked {
		line += " " + self.renderQualifier()
	}

	for _, continuation := range self.Continuation {
		if part := continuation.Render(); part != "" {
			if line != "" {
				line += " "
			}
			line += part
		}
	}

	return link.Render(line, self.PathRoots)
}

func lineRangeStart(lineRange string) string {
	if separator := strings.IndexAny(lineRange, "-+"); separator >= 0 {
		lineRange = lineRange[:separator]
	}
	if _, err := strconv.ParseUint(lineRange, 10, 64); err != nil {
		return ""
	}

	return lineRange
}

func (self Label) Width() int {
	total := width.Of(self.Name)

	if self.Subject != "" {
		total += 1 + width.Of(self.Subject)
	}

	if self.Qualifier != "" {
		total += 1 + width.Of(self.Qualifier)
	}

	for _, continuation := range self.Continuation {
		partWidth := continuation.Width()
		if partWidth == 0 {
			continue
		}
		if total > 0 {
			total++
		}
		total += partWidth
	}

	return total
}

func (self Label) elide(room int) Label {
	self.renderedSubject = ""
	self.Name = width.Elide(self.Name, room)
	room -= width.Of(self.Name) + 1

	if room > 0 {
		completeSubject := self.Subject
		emphasisSource := self.getSource()
		self.Subject = width.Elide(self.Subject, room)
		if self.Emphasis.Kind == tool.EmphasisSyntax && self.Subject != completeSubject {
			subject := strings.TrimSuffix(self.Subject, width.Ellipsis)
			self.renderedSubject = markdown.Highlight(emphasisSource, subject, self.Emphasis.Value, true)
		}
		room -= width.Of(self.Subject) + 1
	} else {
		self.Subject = ""
		room = 0
	}

	if room > 0 && self.Qualifier != "" {
		self.Qualifier = width.Elide(self.Qualifier, room)
		room -= width.Of(self.Qualifier) + 1
	} else {
		self.Qualifier = ""
	}

	continuation := slices.Clone(self.Continuation)
	self.Continuation = make([]Label, 0, len(continuation))
	for _, part := range continuation {
		if room <= 0 {
			break
		}

		part = part.elide(room)
		partWidth := part.Width()
		if partWidth == 0 {
			break
		}

		self.Continuation = append(self.Continuation, part)
		room -= partWidth + 1
	}

	return self
}

func (self Label) getSource() string {
	if self.Emphasis.Source != "" {
		return self.Emphasis.Source
	}

	return self.Subject
}

func (self Label) renderSubject() string {
	if self.renderedSubject != "" {
		return self.renderedSubject
	}
	if self.Emphasis.Kind == tool.EmphasisSyntax {
		return markdown.Highlight(self.getSource(), self.Subject, self.Emphasis.Value, false)
	}

	type span struct {
		start int
		end   int
		style style.Style
	}

	spans := []span{}
	focus := self.focus()
	if at := strings.LastIndex(self.Subject, focus); focus != "" && at >= 0 {
		spans = append(spans, span{start: at, end: at + len(focus), style: style.Subject})
	}
	if at := strings.LastIndex(self.Subject, self.Accent); self.Accent != "" && self.AccentStyle != nil && at >= 0 {
		spans = append(spans, span{start: at, end: at + len(self.Accent), style: self.AccentStyle})
	}

	if len(spans) == 0 {
		return renderScratchAlias(self.Subject, style.Subject)
	}

	slices.SortFunc(spans, func(first span, second span) int {
		return cmp.Compare(first.start, second.start)
	})

	var out strings.Builder
	at := 0
	for _, markedSpan := range spans {
		if markedSpan.start < at {
			continue
		}
		out.WriteString(renderScratchAlias(self.Subject[at:markedSpan.start], style.Subtle))
		out.WriteString(renderScratchAlias(self.Subject[markedSpan.start:markedSpan.end], markedSpan.style))
		at = markedSpan.end
	}
	if at < len(self.Subject) {
		out.WriteString(renderScratchAlias(self.Subject[at:], style.Subtle))
	}

	return out.String()
}

func renderScratchAlias(text string, textStyle style.Style) string {
	rest, hasAlias := strings.CutPrefix(text, link.ScratchAlias)
	if !hasAlias {
		return textStyle(text)
	}

	return style.ScratchAlias(link.ScratchAlias) + textStyle(rest)
}

func (self Label) focus() string {
	if self.Emphasis.Kind == tool.EmphasisFocus {
		return self.Emphasis.Value
	}

	return ""
}

func (self Label) renderQualifier() string {
	focus := self.focus()
	if focus == "" || strings.Contains(self.Subject, focus) {
		return renderScratchAlias(self.Qualifier, style.Qualifier)
	}

	at := strings.LastIndex(self.Qualifier, focus)
	if at < 0 {
		return renderScratchAlias(self.Qualifier, style.Qualifier)
	}

	end := at + len(focus)

	var out strings.Builder
	if at > 0 {
		out.WriteString(renderScratchAlias(self.Qualifier[:at], style.Qualifier))
	}
	out.WriteString(renderScratchAlias(self.Qualifier[at:end], style.Subject))
	if end < len(self.Qualifier) {
		out.WriteString(renderScratchAlias(self.Qualifier[end:], style.Qualifier))
	}

	return out.String()
}

func (self Label) style() style.Style {
	if self.NameStyle != nil {
		return self.NameStyle
	}
	if self.ReadOnly {
		return style.Call
	}

	return style.Change
}

func Measurements(metrics *tool.ToolCallMetrics) string {
	if metrics == nil {
		return ""
	}

	switch metrics.Kind {
	case tool.MetricOutput:
		return outputMetricsText(metrics)
	case tool.MetricResources:
		return resourcesMetricsText(metrics)
	case tool.MetricRead:
		return readMetricsText(metrics)
	case tool.MetricList:
		return listMetricsText(metrics)
	case tool.MetricImage:
		return imageMetricsText(metrics)
	case tool.MetricWrite:
		return writeMetricsText(metrics)
	case tool.MetricDiff:
		return diffMetricsText(metrics)
	case tool.MetricSearch:
		return searchMetricsText(metrics)
	}

	return ""
}

func outputMetricsText(metrics *tool.ToolCallMetrics) string {
	return style.Subtle(outputMeasure(metrics))
}

func outputMeasure(metrics *tool.ToolCallMetrics) string {
	if metrics.Bytes == 0 && metrics.Lines == 0 {
		return "no output"
	}

	truncationMarker := ""
	if metrics.IsTruncated {
		truncationMarker = "+"
	}

	return util.JoinNonEmpty(fmt.Sprintf("%dL%s", metrics.Lines, truncationMarker), tokenEstimate(metrics))
}

func resourcesMetricsText(metrics *tool.ToolCallMetrics) string {
	cpuTime := util.CompactDuration(metrics.CPUTime)
	if cpuTime == noTimeAtAll {
		cpuTime = ""
	}

	peakMemory := ""
	if megabytes := metrics.PeakMemory / bytesPerMegabyte; megabytes > 0 {
		peakMemory = strconv.FormatUint(megabytes, 10) + "M"
	}

	return style.Subtle.Join(cpuTime, outputMeasure(metrics), peakMemory)
}

func readMetricsText(metrics *tool.ToolCallMetrics) string {
	return style.Subtle.Join(strconv.FormatInt(metrics.Lines, 10)+"L", tokenEstimate(metrics))
}

func listMetricsText(metrics *tool.ToolCallMetrics) string {
	return style.Subtle(strconv.FormatInt(metrics.Lines, 10) + "L")
}

func imageMetricsText(metrics *tool.ToolCallMetrics) string {
	return style.Subtle(util.FormatEstimatedTokens(metrics.EstimatedTokens))
}

func writeMetricsText(metrics *tool.ToolCallMetrics) string {
	return style.Subtle.Join(strconv.FormatInt(metrics.Lines, 10)+"L", tokenEstimate(metrics))
}

func diffMetricsText(metrics *tool.ToolCallMetrics) string {
	return style.Success("+%d", metrics.AddedLines) +
		style.Subtle(" ") + style.Failure("−%d", metrics.RemovedLines)
}

func searchMetricsText(metrics *tool.ToolCallMetrics) string {
	capMarker := ""
	if metrics.IsTruncated {
		capMarker = "+"
	}
	return style.Subtle.Join(fmt.Sprintf("%dL%s", metrics.Lines, capMarker), tokenEstimate(metrics))
}

func tokenEstimate(metrics *tool.ToolCallMetrics) string {
	const maximumHiddenTokenEstimate = 100

	returnedTokens := util.EstimateTokenCount(metrics.Bytes)
	isTotalSaid := metrics.TotalBytes > metrics.Bytes &&
		util.EstimateTokenCount(metrics.TotalBytes) > maximumHiddenTokenEstimate

	if returnedTokens <= maximumHiddenTokenEstimate && !isTotalSaid {
		return ""
	}

	returnedText := util.FormatEstimatedTokens(returnedTokens)
	if isTotalSaid {
		return returnedText + " (of " + util.FormatEstimatedTokens(util.EstimateTokenCount(metrics.TotalBytes)) + ")"
	}

	return returnedText
}
