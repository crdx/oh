package dynamic

import (
	"strings"
	"sync"
	"time"

	"crdx.org/oh/internal/app/spinner"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
)

const (
	reveal   = time.Second
	patience = 5 * time.Second
)

type Label interface {
	Elide(room int) Label
	Render() string
	Width() int
}

type LeadingLabel interface {
	Label

	LeadWidth() int
}

type AsideLabel interface {
	Label

	GetAside() string
}

func asideText(label Label) string {
	asideLabel, hasAside := label.(AsideLabel)
	if !hasAside || asideLabel.GetAside() == "" {
		return ""
	}

	return "(" + asideLabel.GetAside() + ")"
}

func leadWidth(label Label) int {
	leadingLabel, isLeading := label.(LeadingLabel)
	if !isLeading {
		return 0
	}

	return leadingLabel.LeadWidth()
}

type RowState int

const (
	Running RowState = iota
	Done
	Succeeded
	Failed
	Cancelled
)

type row struct {
	label     Label
	state     RowState
	startedAt time.Time
	timeLimit time.Duration
	picture   *pictureRows

	timeTaken time.Duration
	summary   string
	metrics   string
}

type Block struct {
	refresh      func()
	mutex        sync.Mutex
	rows         []row
	spinnerFrame int
	isSlow       bool
	heldAt       time.Time
	stop         chan struct{}
	stopWait     sync.WaitGroup
}

func NewBlock(refresh func()) *Block {
	self := &Block{
		refresh: refresh,
		stop:    make(chan struct{}),
	}

	self.stopWait.Add(1)

	go self.run()

	return self
}

func (self *Block) Add(label Label, timeLimit time.Duration) int {
	index := 0

	self.change(func() {
		self.rows = append(self.rows, row{label: label, startedAt: self.startedNow(), timeLimit: timeLimit})
		index = len(self.rows) - 1
	})

	return index
}

func RunningLine(label Label, elapsedTime time.Duration, timeLimit time.Duration, columns int) string {
	startedAt := time.Unix(0, 0)
	block := Block{
		isSlow:       elapsedTime >= reveal,
		spinnerFrame: int(elapsedTime / spinner.Activity.RefreshInterval()),
		heldAt:       startedAt.Add(elapsedTime),
	}

	return block.line(row{label: label, startedAt: startedAt, timeLimit: timeLimit}, columns)
}

func (self *Block) HoldTiming() {
	self.change(func() {
		if self.heldAt.IsZero() {
			self.heldAt = time.Now()
		}
	})
}

func (self *Block) ResumeTiming() {
	self.change(func() {
		if self.heldAt.IsZero() {
			return
		}

		waitedTime := time.Since(self.heldAt)
		self.heldAt = time.Time{}

		for i := range self.rows {
			if self.rows[i].state == Running {
				self.rows[i].startedAt = self.rows[i].startedAt.Add(waitedTime)
			}
		}
	})
}

func (self *Block) Rows(columns int) []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	rows := make([]string, 0, len(self.rows))

	for _, item := range self.rows {
		rows = append(rows, self.line(item, columns))

		if item.picture != nil {
			rows = append(rows, item.picture.render(columns)...)
		}
	}

	return rows
}

func (self *Block) FinaliseRow(
	rowIndex int,
	state RowState,
	timeTaken time.Duration,
	summary string,
	metrics string,
) {
	self.finaliseRow(rowIndex, nil, state, timeTaken, summary, metrics)
}

func (self *Block) FinaliseRowWithLabel(
	rowIndex int,
	label Label,
	state RowState,
	timeTaken time.Duration,
	summary string,
	metrics string,
) {
	self.finaliseRow(rowIndex, label, state, timeTaken, summary, metrics)
}

const widestSummaryRead = 1024

func summarise(text string) string {
	if len(text) > widestSummaryRead {
		text = strings.ToValidUTF8(text[:widestSummaryRead], "")
	}

	return strutil.Flatten(text)
}

func (self *Block) Stop() {
	close(self.stop)
	self.stopWait.Wait()
}

func (self *Block) Close(state RowState) {
	self.Stop()

	self.change(func() {
		for i := range self.rows {
			if self.rows[i].state == Running {
				self.rows[i].state = state
				self.rows[i].timeTaken = self.elapsedTime(self.rows[i])
			}
		}

		self.isSlow = false
	})
}

func (self *Block) finaliseRow(
	rowIndex int,
	label Label,
	state RowState,
	timeTaken time.Duration,
	summary string,
	metrics string,
) {
	self.change(func() {
		if rowIndex < 0 || rowIndex >= len(self.rows) || self.rows[rowIndex].state != Running {
			return
		}

		if label != nil {
			self.rows[rowIndex].label = label
		}
		self.rows[rowIndex].state = state
		self.rows[rowIndex].timeTaken = timeTaken
		self.rows[rowIndex].metrics = metrics
		self.rows[rowIndex].summary = summarise(summary)
	})
}

func (self *Block) change(mutate func()) {
	self.mutex.Lock()
	mutate()
	self.mutex.Unlock()

	self.refresh()
}

func (self *Block) run() {
	defer self.stopWait.Done()

	select {
	case <-self.stop:
		return
	case <-time.After(reveal):
	}

	self.change(func() { self.isSlow = true })

	ticker := time.NewTicker(spinner.Activity.RefreshInterval())
	defer ticker.Stop()

	for {
		select {
		case <-self.stop:
			return
		case <-ticker.C:
			if self.isHeld() {
				continue
			}

			self.change(func() { self.spinnerFrame++ })
		}
	}
}

func (self *Block) isHeld() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return !self.heldAt.IsZero()
}

const failureShare = 2

func (self *Block) line(row row, columns int) string {
	label := row.label
	summary := row.summary
	markRoom := 0
	if mark := self.getProgressIndicator(row); mark != "" {
		markRoom = style.Width(mark) + 1
	}
	lead := min(leadWidth(label), max(columns-markRoom, 0))

	result := ""
	if columns <= 0 || columns > lead {
		result = self.fitResult(row, columns-lead, label.Width()-lead)
	}

	if columns > 0 {
		room := columns - style.Width(result) - resultSpacing(result)

		summary = width.Elide(summary, summaryRoom(row.state, room-lead, label.Width()-lead))

		if summary != "" {
			room -= width.Of(summary) + 1
		}

		label = label.Elide(room)
	}

	line := util.JoinNonEmpty(label.Render(), result, summaryText(row.state, summary))
	if aside := asideText(row.label); aside != "" && (columns <= 0 || style.Width(line)+1+width.Of(aside) <= columns) {
		line = util.JoinNonEmpty(line, style.Reasoning(aside))
	}

	return line
}

func summaryRoom(state RowState, room int, labelWidth int) int {
	spare := room - labelWidth - 1

	if state == Failed || state == Cancelled {
		return max(spare, room/failureShare)
	}

	return spare
}

func summaryText(state RowState, summary string) string {
	if summary == "" {
		return ""
	}

	if state == Failed {
		return style.Failure(summary)
	}

	return style.Subtle(summary)
}

func resultSpacing(result string) int {
	if result == "" {
		return 0
	}

	return 1
}

func (self *Block) fitResult(row row, columns int, labelWidth int) string {
	result := self.getResult(row)

	if columns <= 0 || style.Width(result)+labelGuard(result, labelWidth) <= columns {
		return result
	}

	if mark := self.getProgressIndicator(row); style.Width(mark) <= columns {
		return mark
	}

	return ""
}

func labelGuard(result string, labelWidth int) int {
	if labelWidth == 0 {
		return 0
	}

	return resultSpacing(result) + 1
}

func (self *Block) startedNow() time.Time {
	if self.heldAt.IsZero() {
		return time.Now()
	}

	return self.heldAt
}

func (self *Block) elapsedTime(item row) time.Duration {
	if self.heldAt.IsZero() {
		return time.Since(item.startedAt)
	}

	return self.heldAt.Sub(item.startedAt)
}

func (self *Block) getResult(row row) string {
	if row.state == Running {
		return getResultText(
			self.getProgressIndicator(row),
			self.elapsedTime(row).Truncate(time.Second),
			row.timeLimit,
			"",
		)
	}

	return getResultText(self.getProgressIndicator(row), row.timeTaken, 0, row.metrics)
}

func (self *Block) getProgressIndicator(row row) string {
	if row.state != Running {
		return glyph(row.state)
	}

	if !self.isSlow {
		return ""
	}

	return style.Spinner(spinner.Activity.Frame(self.spinnerFrame))
}

func getResultText(mark string, took time.Duration, timeLimit time.Duration, measuredText string) string {
	waitedText := ""
	if took >= patience {
		waitedText = style.Spinner(util.CompactDuration(took))
		if timeLimit > 0 {
			waitedText += style.Subtle("/" + util.CompactDuration(timeLimit))
		}
	}

	return util.JoinNonEmpty(mark, waitedText, measuredText)
}

func glyph(state RowState) string {
	switch state {
	case Failed:
		return style.Failure("✗")
	case Cancelled:
		return style.CancelledCall("–")
	case Succeeded:
		return style.Success("✓")
	case Running, Done:
		return ""
	}

	return ""
}
