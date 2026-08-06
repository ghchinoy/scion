// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/GoogleCloudPlatform/scion/pkg/agent/state"
	"github.com/GoogleCloudPlatform/scion/pkg/api"
	"github.com/GoogleCloudPlatform/scion/pkg/config"
)

// dashboardSortFields is the cycle of sort fields the 's' key rotates through.
// The empty string means "no explicit sort" (fetch/insertion order). The named
// fields are a subset of validSortFields (list.go) so that sortAgentsByField
// can be reused as-is.
var dashboardSortFields = []string{"", "name", "phase", "created", "updated", "last-seen"}

// agentsMsg carries the result of a fetch back into the Bubble Tea event loop.
type agentsMsg struct {
	agents []api.AgentInfo
	err    error
}

// tickMsg is emitted by the refresh timer to trigger the next fetch.
type tickMsg time.Time

// dashboardModel is the Bubble Tea model backing `scion dashboard`.
type dashboardModel struct {
	// hubCtx is non-nil when the dashboard is reading from the Hub; nil for
	// local runtime mode. Resolved once at construction time.
	hubCtx   *HubContext
	interval time.Duration

	// agents holds the most recent fetch result (unfiltered, unsorted). The
	// rows actually shown are derived on demand by visibleAgents().
	agents []api.AgentInfo

	selected int  // index into the visible (filtered/sorted) rows
	sortIdx  int  // index into dashboardSortFields
	sortDesc bool // reserved: sort direction toggle (not yet bound to a key)

	filtering bool   // true while the '/' filter input is active
	filter    string // current filter query

	// scopePath is the active project scope for fetches. It mirrors the
	// package-level projectPath global that fetchAgentsLocal/fetchAgentsViaHub
	// (and GetProjectID) read; the project switcher updates both together. See
	// dashboard_projects.go.
	scopePath string
	scopeName string // human-friendly label for the active scope

	// Project switcher state (see dashboard_projects.go). The picker is a
	// full-screen panel toggled by 'p'; it does not mutate scope until Enter.
	showProjects bool
	projects     []config.ProjectInfo
	projectsErr  error
	projectSel   int

	// Hub-stats panel state (see dashboard_hub_stats.go). A single-pane summary
	// toggled by 'h'; Hub-mode only. hubStatsLoaded distinguishes "not fetched
	// yet" from "fetched, empty".
	showHubStats   bool
	hubStats       HubStats
	hubStatsErr    error
	hubStatsLoaded bool

	// Look/detail pane state (see dashboard_detail.go). A single-pane, read-only
	// view of one agent's live `tmux capture-pane` output, toggled by Enter on a
	// selected row; Esc returns to the list. detailLoaded distinguishes "not
	// captured yet" from "captured, empty".
	showDetail   bool
	detailName   string // human-friendly agent name (for the pane title)
	detailSlug   string // agent slug used for the capture-pane exec
	detailOutput string
	detailErr    error
	detailLoaded bool

	width  int
	height int

	loading    bool
	lastErr    error
	lastUpdate time.Time
}

// newDashboardModel builds the initial model. hubCtx may be nil (local mode).
func newDashboardModel(hubCtx *HubContext, interval time.Duration) dashboardModel {
	return dashboardModel{
		hubCtx:    hubCtx,
		interval:  interval,
		loading:   true,
		scopePath: projectPath,
		scopeName: resolveScopeName(projectPath),
	}
}

// Init kicks off the first fetch and starts the refresh timer.
func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), m.tickCmd())
}

// fetchCmd runs the appropriate fetch (Hub or local) off the event loop and
// delivers the result as an agentsMsg. It reuses the exact data path shared
// with the `list` command (fetchAgentsLocal / fetchAgentsViaHub).
func (m dashboardModel) fetchCmd() tea.Cmd {
	hubCtx := m.hubCtx
	return func() tea.Msg {
		var (
			agents []api.AgentInfo
			err    error
		)
		if hubCtx != nil {
			agents, err = fetchAgentsViaHub(hubCtx)
		} else {
			agents, err = fetchAgentsLocal()
		}
		return agentsMsg{agents: agents, err: err}
	}
}

// tickCmd arms the refresh timer for one interval.
func (m dashboardModel) tickCmd() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages and returns the next model state.
func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		// Re-fetch and re-arm the timer. When the Hub-stats panel is open in
		// Hub mode, refresh it on the same tick (reusing the Phase 0 interval).
		cmds := []tea.Cmd{m.fetchCmd(), m.tickCmd()}
		if m.showHubStats && m.hubCtx != nil {
			cmds = append(cmds, m.hubStatsCmd())
		}
		// When the detail pane is open, re-capture the agent's terminal output
		// on the same tick so the pane live-updates without a second timer.
		if m.showDetail && m.detailSlug != "" {
			cmds = append(cmds, m.lookCmd())
		}
		return m, tea.Batch(cmds...)

	case lookMsg:
		// Ignore results from a previously-viewed agent (a stale capture that
		// completes after the user switched rows or closed the pane).
		if !m.showDetail || msg.agent != m.detailSlug {
			return m, nil
		}
		m.detailLoaded = true
		m.lastUpdate = time.Now()
		if msg.err != nil {
			m.detailErr = msg.err
			return m, nil
		}
		m.detailErr = nil
		m.detailOutput = msg.output
		return m, nil

	case hubStatsMsg:
		m.hubStatsLoaded = true
		m.lastUpdate = time.Now()
		if msg.err != nil {
			m.hubStatsErr = msg.err
			return m, nil
		}
		m.hubStatsErr = nil
		m.hubStats = msg.stats
		return m, nil

	case agentsMsg:
		m.loading = false
		m.lastUpdate = time.Now()
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		m.lastErr = nil
		m.applyFetch(msg.agents)
		return m, nil

	case rescopeMsg:
		// Result of switching the active project via the picker: adopt the
		// re-resolved Hub context and the first fetch against the new scope.
		m.loading = false
		m.lastUpdate = time.Now()
		m.hubCtx = msg.hubCtx
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		m.lastErr = nil
		m.selected = 0
		m.applyFetch(msg.agents)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey routes a keypress based on whether the filter input is active.
func (m dashboardModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.showDetail {
		return m.handleDetailKey(msg)
	}
	if m.showHubStats {
		return m.handleHubStatsKey(msg)
	}
	if m.showProjects {
		return m.handleProjectKey(msg)
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveSelection(-1)
	case "down", "j":
		m.moveSelection(1)
	case "/":
		m.filtering = true
	case "s":
		m.cycleSort()
	case "p":
		return m.openProjects()
	case "h":
		return m.openHubStats()
	case "enter":
		return m.openDetail()
	}
	return m, nil
}

// handleFilterKey processes keys while the '/' filter input is active.
func (m dashboardModel) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Accept the current filter and leave input mode.
		m.filtering = false
	case "esc":
		// Cancel: clear the filter and leave input mode.
		m.filtering = false
		m.filter = ""
		m.clampSelection()
	case "ctrl+c":
		return m, tea.Quit
	case "backspace":
		if m.filter != "" {
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
			m.clampSelection()
		}
	default:
		// Append printable characters to the filter query.
		if txt := msg.Key().Text; txt != "" {
			m.filter += txt
			m.clampSelection()
		}
	}
	return m, nil
}

// applyFetch stores a fresh fetch result while preserving the current
// selection by Slug so the highlighted row does not jump when the underlying
// data is unchanged.
func (m *dashboardModel) applyFetch(agents []api.AgentInfo) {
	prev := m.selectedSlug()
	m.agents = agents
	if prev != "" {
		visible := m.visibleAgents()
		for i, a := range visible {
			if a.Slug == prev {
				m.selected = i
				return
			}
		}
	}
	m.clampSelection()
}

// selectedSlug returns the Slug of the currently selected visible row, or "".
func (m dashboardModel) selectedSlug() string {
	visible := m.visibleAgents()
	if m.selected >= 0 && m.selected < len(visible) {
		return visible[m.selected].Slug
	}
	return ""
}

// moveSelection moves the selection by delta rows, clamped to valid bounds.
func (m *dashboardModel) moveSelection(delta int) {
	n := len(m.visibleAgents())
	if n == 0 {
		m.selected = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= n {
		m.selected = n - 1
	}
}

// clampSelection keeps the selection index within the visible row range.
func (m *dashboardModel) clampSelection() {
	n := len(m.visibleAgents())
	if n == 0 {
		m.selected = 0
		return
	}
	if m.selected >= n {
		m.selected = n - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// cycleSort advances to the next sort field and applies it via the reused
// sortAgentsByField helper (through visibleAgents()).
func (m *dashboardModel) cycleSort() {
	m.sortIdx = (m.sortIdx + 1) % len(dashboardSortFields)
	m.clampSelection()
}

// visibleAgents returns the rows to display: the fetched agents with friendly
// template names, run through the shared filterAgentsByFlags helper, the
// interactive '/' substring filter, and the shared sortAgentsByField helper.
func (m dashboardModel) visibleAgents() []api.AgentInfo {
	// Copy so we never mutate the stored fetch result.
	out := make([]api.AgentInfo, len(m.agents))
	copy(out, m.agents)

	// Resolve human-friendly template names, mirroring displayAgents.
	for i := range out {
		out[i].Template = config.FriendlyTemplateName(out[i].Template)
	}

	// Reuse the list command's flag-based filter as-is (a no-op unless the
	// package-level filter flags are set).
	out = filterAgentsByFlags(out)

	// Interactive '/' substring filter across the same fields list.go filters
	// on (phase, activity, template) plus name for convenience.
	if q := strings.ToLower(strings.TrimSpace(m.filter)); q != "" {
		filtered := out[:0]
		for _, a := range out {
			if strings.Contains(strings.ToLower(a.Name), q) ||
				strings.Contains(strings.ToLower(a.Template), q) ||
				strings.Contains(strings.ToLower(a.Phase), q) ||
				strings.Contains(strings.ToLower(a.Activity), q) {
				filtered = append(filtered, a)
			}
		}
		out = filtered
	}

	// Reuse the list command's sort as-is. sortAgentsByField reads the
	// package-level sortField/sortReverse; set them for the duration of the
	// call and restore afterward so we don't disturb the `list` command's
	// globals.
	if field := dashboardSortFields[m.sortIdx]; field != "" {
		savedField, savedReverse := sortField, sortReverse
		sortField, sortReverse = field, m.sortDesc
		sortAgentsByField(out)
		sortField, sortReverse = savedField, savedReverse
	}

	return out
}

// ---- View ----------------------------------------------------------------

var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleFooter   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	styleError    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// phaseStyle returns a lipgloss style colored by lifecycle phase.
func phaseStyle(phase string) lipgloss.Style {
	switch state.Phase(strings.ToLower(phase)) {
	case state.PhaseRunning:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	case state.PhaseError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red
	case state.PhaseStopped:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray
	case state.PhaseSuspended:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	default:
		return lipgloss.NewStyle()
	}
}

// dashboard column widths (content only; a two-space gutter separates columns).
const (
	colName     = 24
	colTemplate = 20
	colPhase    = 12
	colActivity = 18
	colLastAct  = 20
)

// View renders the dashboard.
func (m dashboardModel) View() tea.View {
	if m.showDetail {
		view := tea.NewView(m.detailView())
		view.AltScreen = true
		return view
	}
	if m.showHubStats {
		view := tea.NewView(m.hubStatsView())
		view.AltScreen = true
		return view
	}
	if m.showProjects {
		view := tea.NewView(m.projectsView())
		view.AltScreen = true
		return view
	}

	var b strings.Builder

	mode := "local"
	if m.hubCtx != nil {
		mode = "hub"
	}
	title := fmt.Sprintf("scion dashboard  (%s, project: %s, refresh %s)", mode, m.scopeLabel(), m.interval)
	b.WriteString(styleTitle.Render(title))
	b.WriteString("\n\n")

	// Header row.
	header := strings.Join([]string{
		padCell("NAME", colName),
		padCell("TEMPLATE", colTemplate),
		padCell("PHASE", colPhase),
		padCell("ACTIVITY", colActivity),
		padCell("LAST ACTIVITY", colLastAct),
	}, "  ")
	b.WriteString(styleHeader.Render(header))
	b.WriteString("\n")

	visible := m.visibleAgents()
	switch {
	case m.loading && len(visible) == 0:
		b.WriteString(styleDim.Render("Loading agents…"))
		b.WriteString("\n")
	case len(visible) == 0:
		if m.filter != "" {
			b.WriteString(styleDim.Render("No agents match the current filter."))
		} else {
			b.WriteString(styleDim.Render("No active agents found in the current project."))
		}
		b.WriteString("\n")
	default:
		for i, a := range visible {
			b.WriteString(m.renderRow(a, i == m.selected))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.lastErr != nil {
		b.WriteString(styleError.Render(fmt.Sprintf("Error: %v", m.lastErr)))
		b.WriteString("\n")
	}
	b.WriteString(m.renderFooter(len(visible)))

	view := tea.NewView(b.String())
	view.AltScreen = true
	return view
}

// renderRow renders a single agent row, highlighting it if selected.
func (m dashboardModel) renderRow(a api.AgentInfo, selected bool) string {
	phase := a.Phase
	if phase == "" {
		phase = "unknown"
	}

	activityTime := a.LastActivityEvent
	if activityTime.IsZero() {
		activityTime = a.LastSeen
	}

	name := padCell(a.Name, colName)
	template := padCell(a.Template, colTemplate)
	phaseCell := padCell(phase, colPhase)
	activity := padCell(a.Activity, colActivity)
	lastAct := padCell(formatLastSeen(activityTime), colLastAct)

	if selected {
		// Whole row highlighted; skip per-phase coloring to keep contrast.
		row := strings.Join([]string{name, template, phaseCell, activity, lastAct}, "  ")
		return styleSelected.Render(row)
	}

	phaseCell = phaseStyle(phase).Render(phaseCell)
	return strings.Join([]string{name, template, phaseCell, activity, lastAct}, "  ")
}

// renderFooter renders the status/help line at the bottom of the screen.
func (m dashboardModel) renderFooter(count int) string {
	if m.filtering {
		return styleFooter.Render(fmt.Sprintf("/%s█  (enter: apply  esc: cancel)", m.filter))
	}

	sortLabel := dashboardSortFields[m.sortIdx]
	if sortLabel == "" {
		sortLabel = "none"
	}
	filterLabel := "off"
	if m.filter != "" {
		filterLabel = m.filter
	}

	help := fmt.Sprintf(
		"%d agents  sort: %s  filter: %s  |  ↑/↓ j/k move · enter look · / filter · s sort · p projects · h hub · q quit",
		count, sortLabel, filterLabel,
	)
	return styleFooter.Render(help)
}

// padCell truncates or right-pads s to exactly width display columns.
func padCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > width {
		if width <= 1 {
			return string(r[:width])
		}
		return string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(r))
}
