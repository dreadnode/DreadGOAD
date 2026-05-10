package scoreboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Dreadnode color palette.
const (
	cSuccess    = "#68c147"
	cError      = "#e44f4f"
	cWarning    = "#c8ac4a"
	cInfo       = "#4689bf"
	cBrand      = "#ca5e44"
	cFG         = "#e2e7ec"
	cFGMuted    = "#9da0a5"
	cFGFaintest = "#686d73"
)

var (
	styleTitle    = lipgloss.NewStyle().Foreground(lipgloss.Color(cBrand)).Bold(true)
	styleBorder   = lipgloss.NewStyle().Foreground(lipgloss.Color(cBrand))
	styleGroupHdr = lipgloss.NewStyle().Foreground(lipgloss.Color(cBrand)).Bold(true)
	styleAchieved = lipgloss.NewStyle().Foreground(lipgloss.Color(cSuccess)).Bold(true)
	styleTotal    = lipgloss.NewStyle().Foreground(lipgloss.Color(cInfo))
	styleSep      = lipgloss.NewStyle().Foreground(lipgloss.Color(cFGFaintest))
	styleMuted    = lipgloss.NewStyle().Foreground(lipgloss.Color(cFGMuted))
	styleFaint    = lipgloss.NewStyle().Foreground(lipgloss.Color(cFGFaintest))
	styleFG       = lipgloss.NewStyle().Foreground(lipgloss.Color(cFG))
	styleOK       = lipgloss.NewStyle().Foreground(lipgloss.Color(cSuccess)).Bold(true)
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color(cWarning)).Bold(true)
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color(cError)).Bold(true)
	styleInfo     = lipgloss.NewStyle().Foreground(lipgloss.Color(cInfo)).Bold(true)
)

var groupTitles = map[string]string{
	"credentials": "CREDENTIALS DISCOVERED",
	"hosts":       "HOSTS COMPROMISED",
	"domains":     "DOMAINS OWNED",
	"techniques":  "ATTACK TECHNIQUES USED",
}

var groupShort = map[string]string{
	"credentials": "CREDENTIALS",
	"hosts":       "HOSTS",
	"domains":     "DOMAINS",
	"techniques":  "ATTACK TECHNIQUES",
}

var leftGroups = []string{"domains", "hosts", "techniques"}
var rightGroups = []string{"credentials"}

type pollResult int

const (
	pollWaiting pollResult = iota
	pollOK
	pollNoFile
	pollError
)

// TUIConfig configures the live status board.
type TUIConfig struct {
	Transport    Transport
	AnswerKey    *AnswerKey
	PollInterval time.Duration
	ReportPath   string // for display in the footer
}

// RunTUI starts the interactive status board. It returns when the user
// quits (q/ctrl-c) or the context is cancelled.
func RunTUI(ctx context.Context, cfg TUIConfig) error {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	m := newModel(ctx, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

// RenderStatic returns the status board as a single string (used by the demo
// command to print one snapshot without entering an alt-screen TUI).
func RenderStatic(status *StatusReport, ak *AnswerKey, agentID string, startTime time.Time) string {
	width := 120
	return renderBoard(status, ak, agentID, startTime, nil, width)
}

type model struct {
	ctx        context.Context
	cfg        TUIConfig
	status     *StatusReport
	report     *Report
	startTime  time.Time
	width      int
	height     int
	lastPollAt time.Time
	pollState  pollResult
	pollErr    string
	lastHash   uint64
	quitting   bool
}

func newModel(ctx context.Context, cfg TUIConfig) *model {
	empty := &Report{AgentID: "dreadnode-agent"}
	return &model{
		ctx:    ctx,
		cfg:    cfg,
		status: VerifyReport(empty, cfg.AnswerKey),
		report: empty,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.pollCmd(), tickCmd())
}

type pollMsg struct {
	raw  string
	err  error
	when time.Time
}
type tickMsg struct{ t time.Time }

func (m *model) pollCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		raw, err := m.cfg.Transport.FetchReport(ctx)
		return pollMsg{raw: raw, err: err, when: time.Now()}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{t} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, m.pollCmd()
		}
	case pollMsg:
		m.lastPollAt = msg.when
		switch {
		case msg.err == nil:
			m.pollState = pollOK
			m.pollErr = ""
			h := simpleHash(msg.raw)
			if h != m.lastHash {
				m.lastHash = h
				m.report = ParseReport(msg.raw)
				if st, err := time.Parse(time.RFC3339, m.report.StartTime); err == nil && m.startTime.IsZero() {
					m.startTime = st
				}
				m.status = VerifyReport(m.report, m.cfg.AnswerKey)
			}
		case errors.Is(msg.err, ErrNoReport):
			m.pollState = pollNoFile
			m.pollErr = ""
		default:
			m.pollState = pollError
			m.pollErr = msg.err.Error()
		}
		// Schedule next poll
		next := tea.Tick(m.cfg.PollInterval, func(time.Time) tea.Msg {
			return pollKickMsg{}
		})
		return m, next
	case pollKickMsg:
		return m, m.pollCmd()
	case tickMsg:
		return m, tickCmd()
	}
	return m, nil
}

type pollKickMsg struct{}

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 120
	}
	pollSnap := &pollSnapshot{
		state:        m.pollState,
		errMsg:       m.pollErr,
		findingCount: len(m.report.Findings),
		reportPath:   m.cfg.ReportPath,
		lastPollAt:   m.lastPollAt,
		interval:     m.cfg.PollInterval,
	}
	return renderBoard(m.status, m.cfg.AnswerKey, m.report.AgentID, m.startTime, pollSnap, width)
}

type pollSnapshot struct {
	state        pollResult
	errMsg       string
	findingCount int
	reportPath   string
	lastPollAt   time.Time
	interval     time.Duration
}

func renderBoard(status *StatusReport, ak *AnswerKey, agentID string, startTime time.Time, poll *pollSnapshot, width int) string {
	innerWidth := width - 4 // 2 chars border + 2 chars padding (1 each side)
	if innerWidth < 40 {
		innerWidth = 40
	}
	header := renderHeader(status, agentID, startTime, innerWidth)

	colWidth := (innerWidth - 2) / 2
	if colWidth < 30 {
		colWidth = 30
	}
	left := renderColumn(leftGroups, status, ak, colWidth)
	right := renderColumn(rightGroups, status, ak, colWidth)
	cols := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	parts := []string{header, "", cols}
	if len(status.UnmatchedFindings) > 0 {
		parts = append(parts, "",
			styleFaint.Italic(true).Render(fmt.Sprintf("  + %d additional finding(s) reported", len(status.UnmatchedFindings))))
	}
	if poll != nil {
		parts = append(parts, "", renderPollFooter(poll))
		parts = append(parts, styleFaint.Render("  q/ctrl-c quit · r reload"))
	}

	return panelWithTitle("DreadGOAD STATUS BOARD", strings.Join(parts, "\n"), width)
}

// panelWithTitle frames `body` in a rounded border with `title` embedded in
// the top edge.
func panelWithTitle(title, body string, width int) string {
	innerWidth := width - 4 // border (2) + padding (2)
	if innerWidth < 1 {
		innerWidth = 1
	}

	titleText := " " + title + " "
	titleVis := lipgloss.Width(titleText)
	leadDashes := 2
	trailDashes := innerWidth + 2 - leadDashes - titleVis
	if trailDashes < 1 {
		trailDashes = 1
	}
	top := styleBorder.Render("╭"+strings.Repeat("─", leadDashes)) +
		styleTitle.Render(titleText) +
		styleBorder.Render(strings.Repeat("─", trailDashes)+"╮")

	bottom := styleBorder.Render("╰" + strings.Repeat("─", innerWidth+2) + "╯")

	var rows []string
	rows = append(rows, top)
	for _, line := range strings.Split(body, "\n") {
		pad := innerWidth - lipgloss.Width(line)
		if pad < 0 {
			line = truncate(line, innerWidth)
			pad = 0
		}
		rows = append(rows, styleBorder.Render("│")+" "+line+strings.Repeat(" ", pad)+" "+styleBorder.Render("│"))
	}
	rows = append(rows, bottom)
	return strings.Join(rows, "\n")
}

func renderHeader(status *StatusReport, agentID string, startTime time.Time, width int) string {
	left := strings.Builder{}
	first := true
	groupOrder := []string{"credentials", "hosts", "domains", "techniques"}
	for _, g := range groupOrder {
		stats, ok := status.Groups[g]
		if !ok {
			continue
		}
		if !first {
			left.WriteString(styleSep.Render("  |  "))
		}
		first = false
		short := groupShort[g]
		if short == "" {
			short = strings.ToUpper(g)
		}
		left.WriteString(styleGroupHdr.Render(short + " "))
		left.WriteString(styleAchieved.Render(fmt.Sprintf("%d", stats.Achieved)))
		left.WriteString(styleFG.Render("/"))
		left.WriteString(styleTotal.Render(fmt.Sprintf("%d", stats.Total)))
	}

	elapsed := "--:--:--"
	if !startTime.IsZero() {
		elapsed = formatDuration(time.Since(startTime))
	}
	right := styleMuted.Render(fmt.Sprintf("Agent: %s  |  %s", agentID, elapsed))

	leftStr := left.String()
	pad := width - lipgloss.Width(leftStr) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return leftStr + strings.Repeat(" ", pad) + right
}

func renderColumn(groups []string, status *StatusReport, ak *AnswerKey, width int) string {
	var sections []string
	for _, g := range groups {
		stats, ok := status.Groups[g]
		if !ok || stats.Total == 0 {
			continue
		}
		sections = append(sections, renderGroupSection(g, stats, status.Verified, ak, width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func renderGroupSection(group string, stats *GroupStats, verified []VerifiedObjective, ak *AnswerKey, width int) string {
	title := groupTitles[group]
	if title == "" {
		title = strings.ToUpper(group)
	}
	hdr := styleGroupHdr.Render(fmt.Sprintf("  %s  (%d/%d)", title, stats.Achieved, stats.Total))

	achieved := map[string]VerifiedObjective{}
	for _, vo := range verified {
		if vo.Group == group && vo.Verified {
			achieved[vo.ObjectiveID] = vo
		}
	}

	rowWidth := width
	timeColWidth := 10
	statusColWidth := 4
	labelWidth := rowWidth - timeColWidth - statusColWidth - 2
	if labelWidth < 10 {
		labelWidth = 10
	}

	var rows []string
	for _, obj := range ak.Objectives {
		if obj.Group != group {
			continue
		}
		vo, ok := achieved[obj.ID]
		var statusCell, labelCell, timeCell string
		if ok {
			statusCell = styleOK.Render("[x] ")
			labelCell = styleFG.Render(truncate(obj.Label, labelWidth))
			timeCell = styleMuted.Render(formatTS(vo.Timestamp))
		} else {
			statusCell = styleFaint.Render("[ ] ")
			label := obj.Label
			if obj.Hint != "" {
				label = fmt.Sprintf("%s  (%s)", label, obj.Hint)
			}
			labelCell = styleFaint.Render(truncate(label, labelWidth))
			timeCell = ""
		}
		labelCell = padRight(labelCell, labelWidth)
		timeCell = padRight(timeCell, timeColWidth)
		rows = append(rows, statusCell+labelCell+timeCell)
	}
	return hdr + "\n" + strings.Join(rows, "\n") + "\n"
}

func renderPollFooter(p *pollSnapshot) string {
	since := time.Since(p.lastPollAt)
	if p.lastPollAt.IsZero() {
		since = 0
	}
	next := p.interval - since
	if next < 0 {
		next = 0
	}

	b := strings.Builder{}
	switch p.state {
	case pollOK:
		b.WriteString(styleOK.Render("  CONNECTED"))
		b.WriteString(styleMuted.Render(fmt.Sprintf("  (%d findings)", p.findingCount)))
	case pollNoFile:
		b.WriteString(styleWarn.Render("  WAITING FOR REPORT"))
		b.WriteString(styleFaint.Render(fmt.Sprintf("  (%s)", p.reportPath)))
	case pollError:
		b.WriteString(styleErr.Render("  FETCH ERROR"))
		if p.errMsg != "" {
			b.WriteString(styleMuted.Render(fmt.Sprintf("  (%s)", truncate(p.errMsg, 80))))
		}
	default:
		b.WriteString(styleInfo.Render("  CONNECTING..."))
	}
	b.WriteString(styleFaint.Render(fmt.Sprintf("  |  next poll: %ds", int(next.Seconds()))))
	return b.String()
}

func formatTS(ts string) string {
	if ts == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Format("15:04:05")
	}
	if len(ts) > 8 {
		return ts[:8]
	}
	return ts
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

func padRight(s string, w int) string {
	pad := w - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return s[:1]
	}
	// naive byte-level truncation; lab labels are ASCII
	if w > len(s) {
		return s
	}
	return s[:w-1] + "…"
}

// simpleHash is a non-cryptographic hash used only to detect report changes.
func simpleHash(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
