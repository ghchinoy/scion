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

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// This file holds the look/detail sub-view of the dashboard (Phase 2). It is a
// single-pane, read-only view of one agent's live `tmux capture-pane` output —
// the same capture `scion look <agent>` prints: Enter on a selected row opens
// it, Esc returns to the agent list. It reuses the shared capture path
// (captureLookOutput, look.go) and refreshes on the dashboard's existing tick
// (dashboard_model.go), so there is no second timer.
//
// This view forwards no input to the agent's session and does not attach; that
// (the `a` keybinding / attach handoff) is Phase 3, out of scope here.

// lookMsg carries the result of a capture-pane read back into the event loop,
// mirroring agentsMsg. agent is the slug the capture was issued for, so stale
// results from a previously-viewed row can be discarded.
type lookMsg struct {
	agent  string
	output string
	err    error
}

// styleDetailPane is the lipgloss border the detail pane draws around the
// captured output, replacing the CLI's manual strings.Repeat border
// (printLookOutput, look.go) with a styled box sized to the terminal.
var styleDetailPane = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("12")).
	Padding(0, 1)

// openDetail opens the look/detail pane for the currently selected agent and
// kicks off the first capture off the event loop. It is a no-op when there is
// no selectable row.
func (m dashboardModel) openDetail() (tea.Model, tea.Cmd) {
	visible := m.visibleAgents()
	if m.selected < 0 || m.selected >= len(visible) {
		return m, nil
	}
	a := visible[m.selected]

	m.showDetail = true
	m.detailName = a.Name
	m.detailSlug = a.Slug
	m.detailOutput = ""
	m.detailErr = nil
	m.detailLoaded = false
	return m, m.lookCmd()
}

// lookCmd captures the selected agent's terminal output off the event loop and
// delivers it as a lookMsg. It reuses the shared captureLookOutput data path
// (look.go) with the same transport (Hub vs local) the rest of the dashboard
// uses. ANSI is stripped (plain) so the raw capture can be framed inside the
// lipgloss border without color escapes bleeding into it.
func (m dashboardModel) lookCmd() tea.Cmd {
	hubCtx := m.hubCtx
	slug := m.detailSlug
	return func() tea.Msg {
		execCmd := buildLookCmd(true /* plain */, false /* full */, 0 /* numLines */)
		output, err := captureLookOutput(hubCtx, slug, execCmd)
		return lookMsg{agent: slug, output: output, err: err}
	}
}

// handleDetailKey processes keys while the detail pane is open. It is read-only:
// only navigation (Esc back) and quit are handled; no input reaches the agent.
func (m dashboardModel) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		// Return to the agent list and drop the captured buffer.
		m.showDetail = false
		m.detailName = ""
		m.detailSlug = ""
		m.detailOutput = ""
		m.detailErr = nil
		m.detailLoaded = false
	}
	return m, nil
}

// detailView renders the full-screen look/detail pane.
func (m dashboardModel) detailView() string {
	var b strings.Builder

	mode := "local"
	if m.hubCtx != nil {
		mode = "hub"
	}
	title := fmt.Sprintf("scion look  (%s)  agent: %s", mode, m.detailName)
	b.WriteString(styleTitle.Render(title))
	b.WriteString("\n\n")

	switch {
	case m.detailErr != nil:
		b.WriteString(styleError.Render(fmt.Sprintf("Error: %v", m.detailErr)))
		b.WriteString("\n")
	case !m.detailLoaded:
		b.WriteString(styleDim.Render("Capturing terminal output…"))
		b.WriteString("\n")
	default:
		b.WriteString(m.renderDetailPane())
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styleFooter.Render(fmt.Sprintf("live · refresh %s · esc back · q quit", m.interval)))
	return b.String()
}

// renderDetailPane frames the captured output in the lipgloss border, sized to
// the current terminal width when known.
func (m dashboardModel) renderDetailPane() string {
	content := strings.TrimRight(m.detailOutput, "\n")
	if strings.TrimSpace(content) == "" {
		content = styleDim.Render("(no terminal output captured)")
	}

	pane := styleDetailPane
	// Leave room for the border (2) and padding (2); fall back to the pane's
	// natural width when the terminal size is not yet known.
	if m.width > 4 {
		pane = pane.Width(m.width - 4)
	}
	return pane.Render(content)
}
