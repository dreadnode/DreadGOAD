package validate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Dreadnode color palette (mirrors cli/internal/scoreboard/tui.go).
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
	styleSep      = lipgloss.NewStyle().Foreground(lipgloss.Color(cFGFaintest))
	styleMuted    = lipgloss.NewStyle().Foreground(lipgloss.Color(cFGMuted))
	styleFaint    = lipgloss.NewStyle().Foreground(lipgloss.Color(cFGFaintest))
	styleFG       = lipgloss.NewStyle().Foreground(lipgloss.Color(cFG))
	styleOK       = lipgloss.NewStyle().Foreground(lipgloss.Color(cSuccess)).Bold(true)
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color(cWarning)).Bold(true)
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color(cError)).Bold(true)
	styleInfo     = lipgloss.NewStyle().Foreground(lipgloss.Color(cInfo)).Bold(true)
)

type categoryStats struct {
	name   string
	pass   int
	fail   int
	warn   int
	total  int // pass + fail + warn (skip/info excluded from totals)
	others int // skip, info, other
}

// RenderSummaryPanel returns a static status board for a completed validation
// run, styled to match the live scoreboard.
func RenderSummaryPanel(r *Report, env, region string, elapsed time.Duration, width int) string {
	if width <= 0 {
		width = 120
	}
	innerWidth := width - 4
	if innerWidth < 40 {
		innerWidth = 40
	}

	header := renderValidateHeader(r, env, region, elapsed, innerWidth)

	cats := aggregateByCategory(r)
	colWidth := (innerWidth - 2) / 2
	if colWidth < 30 {
		colWidth = 30
	}
	left, right := splitForColumns(cats)
	leftCol := renderCategoryColumn(left, colWidth)
	rightCol := renderCategoryColumn(right, colWidth)
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, "  ", rightCol)

	parts := []string{
		header,
		"",
		styleGroupHdr.Render(fmt.Sprintf("  CHECK RESULTS  (%d/%d)", r.Passed, totalForRate(r))),
		"",
		cols,
	}
	if rate, ok := successRate(r); ok {
		parts = append(parts, "", styleMuted.Render(fmt.Sprintf("  Success rate: %d%%", rate)))
	}

	return panelWithTitle("DreadGOAD VALIDATION", strings.Join(parts, "\n"), width)
}

func renderValidateHeader(r *Report, env, region string, elapsed time.Duration, width int) string {
	left := strings.Builder{}
	writeStat := func(label string, n int, valStyle lipgloss.Style, first bool) {
		if !first {
			left.WriteString(styleSep.Render("  |  "))
		}
		left.WriteString(styleGroupHdr.Render(label + " "))
		left.WriteString(valStyle.Render(fmt.Sprintf("%d", n)))
	}
	writeStat("PASSED", r.Passed, styleOK, true)
	writeStat("FAILED", r.Failed, styleErr, false)
	writeStat("WARNED", r.Warnings, styleWarn, false)
	writeStat("TOTAL", totalForRate(r), styleInfo, false)

	rightParts := []string{}
	if env != "" {
		rightParts = append(rightParts, env)
	}
	if region != "" {
		rightParts = append(rightParts, region)
	}
	if elapsed > 0 {
		rightParts = append(rightParts, formatElapsed(elapsed))
	}
	right := styleMuted.Render(strings.Join(rightParts, "  |  "))

	leftStr := left.String()
	pad := width - lipgloss.Width(leftStr) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return leftStr + strings.Repeat(" ", pad) + right
}

func aggregateByCategory(r *Report) []categoryStats {
	if r == nil {
		return nil
	}
	idx := map[string]*categoryStats{}
	for _, res := range r.Results {
		c, ok := idx[res.Category]
		if !ok {
			c = &categoryStats{name: res.Category}
			idx[res.Category] = c
		}
		switch res.Status {
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
	out := make([]categoryStats, 0, len(idx))
	for _, c := range idx {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func splitForColumns(cats []categoryStats) ([]categoryStats, []categoryStats) {
	if len(cats) == 0 {
		return nil, nil
	}
	mid := (len(cats) + 1) / 2
	return cats[:mid], cats[mid:]
}

func renderCategoryColumn(cats []categoryStats, width int) string {
	if len(cats) == 0 {
		return ""
	}
	iconWidth := 4
	countsWidth := 8
	detailWidth := 10
	nameWidth := width - iconWidth - countsWidth - detailWidth - 2
	if nameWidth < 10 {
		nameWidth = 10
	}

	rows := make([]string, 0, len(cats))
	for _, c := range cats {
		var iconCell string
		switch {
		case c.fail > 0:
			iconCell = styleErr.Render("[x] ")
		case c.warn > 0:
			iconCell = styleWarn.Render("[!] ")
		case c.total > 0:
			iconCell = styleOK.Render("[v] ")
		default:
			iconCell = styleFaint.Render("[ ] ")
		}

		var nameCell string
		switch {
		case c.fail > 0, c.total > 0:
			nameCell = styleFG.Render(truncate(c.name, nameWidth))
		default:
			nameCell = styleFaint.Render(truncate(c.name, nameWidth))
		}
		nameCell = padRight(nameCell, nameWidth)

		counts := fmt.Sprintf("%d/%d", c.pass, c.total)
		countsCell := padRight(styleMuted.Render(counts), countsWidth)

		var detail string
		switch {
		case c.fail > 0 && c.warn > 0:
			detail = fmt.Sprintf("x%d !%d", c.fail, c.warn)
		case c.fail > 0:
			detail = fmt.Sprintf("x%d", c.fail)
		case c.warn > 0:
			detail = fmt.Sprintf("!%d", c.warn)
		}
		var detailCell string
		switch {
		case c.fail > 0:
			detailCell = styleErr.Render(detail)
		case c.warn > 0:
			detailCell = styleWarn.Render(detail)
		}
		detailCell = padRight(detailCell, detailWidth)

		rows = append(rows, "  "+iconCell+nameCell+countsCell+detailCell)
	}
	return strings.Join(rows, "\n")
}

func totalForRate(r *Report) int {
	if r == nil {
		return 0
	}
	return r.Passed + r.Failed + r.Warnings
}

func successRate(r *Report) (int, bool) {
	t := totalForRate(r)
	if t == 0 {
		return 0, false
	}
	return r.Passed * 100 / t, true
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, s)
}

// panelWithTitle frames body in a rounded border with title embedded in the
// top edge. Mirrors the scoreboard implementation so the validate summary and
// scoreboard share a consistent visual frame.
func panelWithTitle(title, body string, width int) string {
	innerWidth := width - 4
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
	if w > len(s) {
		return s
	}
	return s[:w-1] + "…"
}
