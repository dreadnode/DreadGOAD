package validate

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TUIConfig configures the live validation dashboard.
type TUIConfig struct {
	// Validator is the active validator. Its OnResult/Silent/Logger are
	// managed by RunTUI for the duration of the run.
	Validator *Validator
	// Env, Region are shown in the header.
	Env, Region string
	// Run executes the validation checks. It is called once per iteration
	// from a goroutine; on each call, the validator is reset and the run
	// reports streamed back to the TUI. Implementations typically call
	// Validator.RunAllChecks or Validator.RunQuickChecks plus any provider
	// drain.
	Run func(context.Context)
	// PollInterval re-runs the checks on this cadence after each pass
	// completes. 0 means one-shot (run once, then wait for the user to
	// quit).
	PollInterval time.Duration
}

// RunTUI launches the live validation dashboard. It returns when the user
// quits (q/ctrl-c/esc) or the context is cancelled. The validator's report is
// the canonical record on exit; callers should save it and print the path
// after RunTUI returns.
func RunTUI(ctx context.Context, cfg TUIConfig) error {
	if cfg.Validator == nil || cfg.Run == nil {
		return fmt.Errorf("validate.RunTUI: Validator and Run are required")
	}

	// Channel sized to absorb bursts: ~50 checks × 16-way concurrent fan-out.
	results := make(chan Result, 256)
	phases := make(chan phaseEvent, 8)

	// Seed the model with results already on the report (e.g. Discovery
	// PASS lines from before the TUI started) so the dashboard reflects
	// total state, not just live deltas.
	seed := snapshotReport(cfg.Validator)

	cfg.Validator.SetSilent(true)
	cfg.Validator.SetOnResult(func(r Result) {
		select {
		case results <- r:
		default:
			// Channel full -- drop the live update; the structured report
			// already has it. The TUI will resync via the validator's
			// report when the run completes.
			_ = r
		}
	})
	// Redirect slog output for the duration of the TUI run. Bubbletea's alt
	// screen does not capture stderr, so the validator's PS-failure Warn
	// lines would otherwise paint on top of the dashboard.
	prevLog := cfg.Validator.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer cfg.Validator.SetLogger(prevLog)
	defer cfg.Validator.SetOnResult(nil)
	defer cfg.Validator.SetSilent(false)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	runDone := make(chan struct{})
	go runLoop(runCtx, cfg, results, phases, runDone)

	m := newValidateModel(cfg, seed, results, phases)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	_, err := p.Run()

	// Make sure the run goroutine exits before we hand control back so the
	// canonical report in v.report is settled.
	cancel()
	<-runDone
	return err
}

// runLoop drives the per-iteration validate -> wait cycle. It owns the
// results/phases channels for its lifetime and closes them on exit so the TUI
// model can detect completion.
func runLoop(ctx context.Context, cfg TUIConfig, results chan<- Result, phases chan<- phaseEvent, done chan<- struct{}) {
	defer close(done)
	defer close(results)
	defer close(phases)

	// First iteration uses the validator state already seeded by
	// DiscoverHosts (the model already has those results too); skip
	// the initial Reset so we don't wipe Discovery.
	for first := true; ; first = false {
		if !first {
			cfg.Validator.Reset()
		}
		if !sendPhase(ctx, phases, phaseEvent{kind: phaseRunning, iteration: time.Now()}) {
			return
		}

		cfg.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		if cfg.PollInterval <= 0 {
			sendPhase(ctx, phases, phaseEvent{kind: phaseDone})
			return
		}

		until := time.Now().Add(cfg.PollInterval)
		if !sendPhase(ctx, phases, phaseEvent{kind: phaseWaiting, deadline: until}) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.PollInterval):
		}
	}
}

// sendPhase forwards an event to the phases channel, returning false if the
// context cancels first.
func sendPhase(ctx context.Context, phases chan<- phaseEvent, ev phaseEvent) bool {
	select {
	case phases <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// snapshotReport copies the validator's report under its mutex.
func snapshotReport(v *Validator) Report {
	r := *v.GetReport()
	cp := make([]Result, len(r.Results))
	copy(cp, r.Results)
	r.Results = cp
	return r
}

type phaseKind int

const (
	phaseRunning phaseKind = iota
	phaseWaiting
	phaseDone
)

type phaseEvent struct {
	kind      phaseKind
	deadline  time.Time // for phaseWaiting
	iteration time.Time // start time, for phaseRunning (used to reset elapsed)
}

type liveModel struct {
	cfg       TUIConfig
	report    Report
	cats      map[string]*categoryStats
	startTime time.Time
	width     int
	height    int

	results <-chan Result
	phases  <-chan phaseEvent

	phase     phaseKind
	waitUntil time.Time
	iteration int
	finished  bool
	quitting  bool

	// Track seeded counts so a Reset between polls can subtract pre-TUI
	// Discovery results (we want the per-iteration view, but the seeded
	// Discovery rows were valid only for the first pass).
	seededReport Report
}

func newValidateModel(cfg TUIConfig, seed Report, results <-chan Result, phases <-chan phaseEvent) *liveModel {
	m := &liveModel{
		cfg:          cfg,
		report:       Report{Env: seed.Env, Date: seed.Date},
		cats:         map[string]*categoryStats{},
		startTime:    time.Now(),
		results:      results,
		phases:       phases,
		phase:        phaseRunning,
		seededReport: seed,
	}
	for _, r := range seed.Results {
		m.applyResult(r)
	}
	return m
}

type liveResultMsg struct{ r Result }
type liveDoneMsg struct{}
type livePhaseMsg struct{ ev phaseEvent }
type livePhaseClosedMsg struct{}
type liveTickMsg struct{}

func (m *liveModel) Init() tea.Cmd {
	return tea.Batch(m.waitForResultCmd(), m.waitForPhaseCmd(), liveTickCmd())
}

func (m *liveModel) waitForResultCmd() tea.Cmd {
	return func() tea.Msg {
		r, ok := <-m.results
		if !ok {
			return liveDoneMsg{}
		}
		return liveResultMsg{r: r}
	}
}

func (m *liveModel) waitForPhaseCmd() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.phases
		if !ok {
			return livePhaseClosedMsg{}
		}
		return livePhaseMsg{ev: ev}
	}
}

func liveTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return liveTickMsg{} })
}

func (m *liveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		}
	case liveResultMsg:
		m.applyResult(msg.r)
		return m, m.waitForResultCmd()
	case liveDoneMsg:
		// Results channel closed; checks goroutine has exited. We may still
		// be waiting on a final phase event.
	case livePhaseMsg:
		m.applyPhase(msg.ev)
		return m, m.waitForPhaseCmd()
	case livePhaseClosedMsg:
		m.finished = true
	case liveTickMsg:
		return m, liveTickCmd()
	}
	return m, nil
}

func (m *liveModel) applyPhase(ev phaseEvent) {
	switch ev.kind {
	case phaseRunning:
		// New iteration: clear the previous pass's per-category state and
		// reset the elapsed-time anchor. Iteration 0 keeps the Discovery
		// seed; later iterations start from zero.
		if m.iteration > 0 {
			m.report = Report{Env: m.report.Env, Date: m.report.Date}
			m.cats = map[string]*categoryStats{}
		}
		m.iteration++
		m.startTime = ev.iteration
		if m.startTime.IsZero() {
			m.startTime = time.Now()
		}
		m.phase = phaseRunning
		m.waitUntil = time.Time{}
	case phaseWaiting:
		m.phase = phaseWaiting
		m.waitUntil = ev.deadline
	case phaseDone:
		m.phase = phaseDone
		m.finished = true
	}
}

func (m *liveModel) applyResult(r Result) {
	m.report.Results = append(m.report.Results, r)
	switch r.Status {
	case "PASS":
		m.report.Passed++
	case "FAIL":
		m.report.Failed++
	case "WARN":
		m.report.Warnings++
	}
	c, ok := m.cats[r.Category]
	if !ok {
		c = &categoryStats{name: r.Category}
		m.cats[r.Category] = c
	}
	switch r.Status {
	case "PASS":
		c.pass++
		c.total++
	case "FAIL":
		c.fail++
		c.total++
	case "WARN":
		c.warn++
		c.total++
	default:
		c.others++
	}
}

func (m *liveModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	width := m.width
	if width <= 0 {
		width = 120
	}
	innerWidth := width - 4
	if innerWidth < 40 {
		innerWidth = 40
	}

	cats := m.sortedCategories()
	colWidth := (innerWidth - 2) / 2
	if colWidth < 30 {
		colWidth = 30
	}
	left, right := splitForColumns(cats)
	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		renderCategoryColumn(left, colWidth),
		"  ",
		renderCategoryColumn(right, colWidth),
	)

	header := renderValidateHeader(&m.report, m.cfg.Env, m.cfg.Region, time.Since(m.startTime), innerWidth)
	subhdr := styleGroupHdr.Render(fmt.Sprintf("  CHECK RESULTS  (%d/%d)", m.report.Passed, m.report.Passed+m.report.Failed+m.report.Warnings))
	footer := m.renderFooter()

	// Compact mode: drop blank spacers and the keyboard hint when the
	// natural layout exceeds the terminal height (e.g. short tmux pane).
	contentRows := 1 + 1 + lipgloss.Height(cols) + 1 + 1 // header, subhdr, cols, footer, hint
	spacerRows := 3                                      // header→subhdr, subhdr→cols, cols→footer
	natural := contentRows + spacerRows + 2              // borders
	compact := m.height > 0 && natural > m.height

	parts := []string{header}
	if !compact {
		parts = append(parts, "")
	}
	parts = append(parts, subhdr)
	if !compact {
		parts = append(parts, "")
	}
	parts = append(parts, cols)
	if !compact {
		parts = append(parts, "")
	}
	parts = append(parts, footer)
	if !compact {
		parts = append(parts, styleFaint.Render("  q/ctrl-c quit"))
	}

	v := tea.NewView(panelWithTitle("DreadGOAD VALIDATION", strings.Join(parts, "\n"), width))
	v.AltScreen = true
	return v
}

func (m *liveModel) sortedCategories() []categoryStats {
	out := make([]categoryStats, 0, len(m.cats))
	for _, c := range m.cats {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func (m *liveModel) renderFooter() string {
	resultCount := len(m.report.Results)
	b := strings.Builder{}
	switch m.phase {
	case phaseWaiting:
		remaining := time.Until(m.waitUntil)
		if remaining < 0 {
			remaining = 0
		}
		b.WriteString(styleWarn.Render("  WAITING"))
		b.WriteString(styleMuted.Render(fmt.Sprintf("  next run in %s  (%d results)",
			formatRemaining(remaining), resultCount)))
	case phaseDone:
		b.WriteString(styleOK.Render("  COMPLETE"))
		b.WriteString(styleMuted.Render(fmt.Sprintf("  (%d results)", resultCount)))
	default: // phaseRunning
		b.WriteString(styleInfo.Render("  RUNNING"))
		if m.iteration > 1 {
			b.WriteString(styleMuted.Render(fmt.Sprintf("  pass #%d  (%d results so far)", m.iteration, resultCount)))
		} else {
			b.WriteString(styleMuted.Render(fmt.Sprintf("  (%d results so far)", resultCount)))
		}
	}
	if rate, ok := successRate(&m.report); ok && (m.phase != phaseRunning || m.report.Failed+m.report.Warnings > 0) {
		b.WriteString(styleSep.Render("  |  "))
		b.WriteString(styleMuted.Render(fmt.Sprintf("success rate: %d%%", rate)))
	}
	if m.cfg.PollInterval > 0 && m.phase != phaseDone {
		b.WriteString(styleSep.Render("  |  "))
		b.WriteString(styleFaint.Render(fmt.Sprintf("poll: %s", m.cfg.PollInterval)))
	}
	return b.String()
}

func formatRemaining(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
