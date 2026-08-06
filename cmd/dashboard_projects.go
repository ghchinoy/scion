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
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/GoogleCloudPlatform/scion/pkg/api"
	"github.com/GoogleCloudPlatform/scion/pkg/config"
)

// This file holds the project-switcher sub-view of the dashboard (Phase 1). It
// is a filter/switch, not a project-management surface: 'p' opens a full-screen
// picker listing the projects discovered by config.DiscoverProjects(), 'esc'
// closes it without changing scope, and 'enter' re-scopes the agent list to the
// selected project and re-triggers the shared fetch path. Create/rename/prune
// intentionally stay in `scion project *`.

// rescopeMsg carries the result of switching the active project: the freshly
// resolved Hub context (nil for local mode) and the first agent fetch against
// the new scope.
type rescopeMsg struct {
	hubCtx *HubContext
	agents []api.AgentInfo
	err    error
}

// project picker column widths (content only; a two-space gutter separates
// columns), mirroring the agent-table layout in dashboard_model.go.
const (
	projColName   = 28
	projColType   = 10
	projColAgents = 8
	projColStatus = 10
)

// openProjects discovers the known projects and opens the picker panel. The
// filesystem scan performed by DiscoverProjects is cheap, so it runs inline on
// the event-loop thread rather than as a tea.Cmd.
func (m dashboardModel) openProjects() (tea.Model, tea.Cmd) {
	m.showProjects = true
	projects, err := config.DiscoverProjects()
	m.projects = projects
	m.projectsErr = err

	// Position the selection on the currently active scope when possible so
	// the picker opens focused on "where you are".
	m.projectSel = 0
	for i, p := range projects {
		if projectScopePath(p) == m.scopePath {
			m.projectSel = i
			break
		}
	}
	return m, nil
}

// handleProjectKey processes keys while the project picker is open.
func (m dashboardModel) handleProjectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		// Close without changing scope.
		m.showProjects = false
	case "up", "k":
		m.moveProjectSelection(-1)
	case "down", "j":
		m.moveProjectSelection(1)
	case "enter":
		return m.selectProject()
	}
	return m, nil
}

// moveProjectSelection moves the picker selection by delta rows, clamped.
func (m *dashboardModel) moveProjectSelection(delta int) {
	n := len(m.projects)
	if n == 0 {
		m.projectSel = 0
		return
	}
	m.projectSel += delta
	if m.projectSel < 0 {
		m.projectSel = 0
	}
	if m.projectSel >= n {
		m.projectSel = n - 1
	}
}

// selectProject re-scopes the dashboard to the highlighted project and triggers
// a fresh fetch against it. It updates both the model's scope fields and the
// package-level projectPath global that the shared fetch functions read. The
// global is written only here, on the single-threaded event loop.
func (m dashboardModel) selectProject() (tea.Model, tea.Cmd) {
	if m.projectSel < 0 || m.projectSel >= len(m.projects) {
		m.showProjects = false
		return m, nil
	}

	p := m.projects[m.projectSel]
	scope := projectScopePath(p)

	m.scopePath = scope
	m.scopeName = p.Name
	m.showProjects = false
	m.loading = true

	// Re-parameterize the shared list data path onto the new project.
	projectPath = scope

	return m, m.rescopeCmd(scope)
}

// rescopeCmd re-resolves the Hub context for the given scope and performs the
// first fetch against it, off the event loop. It mirrors the bootstrap in
// dashboard.go's RunE but for a project selected at runtime. Sync is skipped so
// switching projects does not block the UI on Hub reconciliation.
func (m dashboardModel) rescopeCmd(scope string) tea.Cmd {
	return func() tea.Msg {
		hubCtx, err := CheckHubAvailabilityWithOptions(scope, true)
		if err != nil {
			return rescopeMsg{err: err}
		}
		var agents []api.AgentInfo
		if hubCtx != nil {
			agents, err = fetchAgentsViaHub(hubCtx)
		} else {
			agents, err = fetchAgentsLocal()
		}
		return rescopeMsg{hubCtx: hubCtx, agents: agents, err: err}
	}
}

// projectScopePath returns the value to assign to projectPath when switching to
// the given project. The global project uses the "global" sentinel the config
// layer understands; every other project scopes by its workspace path.
func projectScopePath(p config.ProjectInfo) string {
	if p.Type == config.ProjectTypeGlobal {
		return "global"
	}
	return p.WorkspacePath
}

// resolveScopeName returns a human-friendly label for a scope path. The empty
// string is the current working directory's project.
func resolveScopeName(path string) string {
	switch path {
	case "":
		return "current"
	case "global":
		return "global"
	default:
		return filepath.Base(path)
	}
}

// scopeLabel returns the label to show for the active scope.
func (m dashboardModel) scopeLabel() string {
	if m.scopeName != "" {
		return m.scopeName
	}
	return resolveScopeName(m.scopePath)
}

// projectsView renders the full-screen project picker panel.
func (m dashboardModel) projectsView() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render(fmt.Sprintf("Select project  (current: %s)", m.scopeLabel())))
	b.WriteString("\n\n")

	header := strings.Join([]string{
		padCell("NAME", projColName),
		padCell("TYPE", projColType),
		padCell("AGENTS", projColAgents),
		padCell("STATUS", projColStatus),
	}, "  ")
	b.WriteString(styleHeader.Render(header))
	b.WriteString("\n")

	switch {
	case m.projectsErr != nil:
		b.WriteString(styleError.Render(fmt.Sprintf("Error discovering projects: %v", m.projectsErr)))
		b.WriteString("\n")
	case len(m.projects) == 0:
		b.WriteString(styleDim.Render("No projects found. Run 'scion init' to create one."))
		b.WriteString("\n")
	default:
		for i, p := range m.projects {
			b.WriteString(m.renderProjectRow(p, i == m.projectSel))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(styleFooter.Render("↑/↓ j/k move · enter switch · esc cancel · q quit"))
	return b.String()
}

// renderProjectRow renders a single project row, highlighting the selection and
// dimming orphaned projects (whose workspace no longer exists).
func (m dashboardModel) renderProjectRow(p config.ProjectInfo, selected bool) string {
	row := strings.Join([]string{
		padCell(p.Name, projColName),
		padCell(string(p.Type), projColType),
		padCell(fmt.Sprintf("%d", p.AgentCount), projColAgents),
		padCell(string(p.Status), projColStatus),
	}, "  ")

	if selected {
		return styleSelected.Render(row)
	}
	if p.Status == config.ProjectStatusOrphaned {
		return styleDim.Render(row)
	}
	return row
}
