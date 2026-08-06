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
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// This file holds the Hub-stats sub-view of the dashboard. It is a single-pane
// summary screen (not a scrollable list): 'h' opens it, 'esc' returns to the
// agent list. It is Hub-mode only — in local mode there is no fleet-wide view
// (see cmd/list.go), so the panel shows a clear message instead. Data comes from
// fetchHubStats (hub_stats.go); this file only decides how to present it.

// hubStatsMsg carries the result of a fetchHubStats call back into the event
// loop, mirroring agentsMsg.
type hubStatsMsg struct {
	stats HubStats
	err   error
}

// broker health table column widths (content only; a two-space gutter separates
// columns), mirroring the agent-table layout in dashboard_model.go.
const (
	brokerColName   = 24
	brokerColStatus = 12
	brokerColConn   = 16
	brokerColSeen   = 16
)

// hubStatsBarWidth is the maximum width, in cells, of an agents-by-phase bar.
const hubStatsBarWidth = 24

// openHubStats opens the Hub-stats panel. In Hub mode it kicks off a fetch off
// the event loop; in local mode it records the "requires Hub mode" message and
// fetches nothing.
func (m dashboardModel) openHubStats() (tea.Model, tea.Cmd) {
	m.showHubStats = true
	if m.hubCtx == nil {
		m.hubStatsErr = errHubStatsRequiresHub
		m.hubStatsLoaded = true
		return m, nil
	}
	m.hubStatsErr = nil
	m.hubStatsLoaded = false
	return m, m.hubStatsCmd()
}

// hubStatsCmd fetches fleet-wide Hub statistics off the event loop and delivers
// them as a hubStatsMsg. It reuses the shared fetchHubStats data path.
func (m dashboardModel) hubStatsCmd() tea.Cmd {
	hubCtx := m.hubCtx
	return func() tea.Msg {
		stats, err := fetchHubStats(hubCtx)
		return hubStatsMsg{stats: stats, err: err}
	}
}

// handleHubStatsKey processes keys while the Hub-stats panel is open.
func (m dashboardModel) handleHubStatsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "h":
		// Return to the agent list.
		m.showHubStats = false
	}
	return m, nil
}

// hubStatsView renders the full-screen Hub-stats summary panel.
func (m dashboardModel) hubStatsView() string {
	var b strings.Builder

	endpoint := ""
	if m.hubCtx != nil {
		endpoint = m.hubCtx.Endpoint
	}
	title := "Hub stats"
	if endpoint != "" {
		title = fmt.Sprintf("Hub stats  (endpoint: %s)", endpoint)
	}
	b.WriteString(styleTitle.Render(title))
	b.WriteString("\n\n")

	switch {
	case m.hubStatsErr != nil:
		b.WriteString(styleError.Render(fmt.Sprintf("%v", m.hubStatsErr)))
		b.WriteString("\n")
	case !m.hubStatsLoaded:
		b.WriteString(styleDim.Render("Loading hub stats…"))
		b.WriteString("\n")
	default:
		m.renderHubStatsBody(&b)
	}

	b.WriteString("\n")
	b.WriteString(styleFooter.Render("esc back · q quit"))
	return b.String()
}

// renderHubStatsBody renders the totals, agents-by-phase tally, and broker
// health table for a loaded, error-free snapshot.
func (m dashboardModel) renderHubStatsBody(b *strings.Builder) {
	s := m.hubStats

	// Top-line totals.
	summary := fmt.Sprintf("Projects: %d    Agents: %d    Brokers: %d",
		s.ProjectCount, s.AgentCount, len(s.Brokers))
	b.WriteString(styleHeader.Render(summary))
	b.WriteString("\n\n")

	// Agents by phase (across the whole fleet).
	b.WriteString(styleHeader.Render("Agents by phase"))
	b.WriteString("\n")
	if len(s.AgentsByPhase) == 0 {
		b.WriteString(styleDim.Render("  (no agents)"))
		b.WriteString("\n")
	} else {
		phases := make([]string, 0, len(s.AgentsByPhase))
		maxCount := 0
		for phase, count := range s.AgentsByPhase {
			phases = append(phases, phase)
			if count > maxCount {
				maxCount = count
			}
		}
		sort.Strings(phases)
		for _, phase := range phases {
			count := s.AgentsByPhase[phase]
			bar := hubStatsBar(count, maxCount)
			label := padCell(phase, colPhase)
			line := fmt.Sprintf("  %s %3d  %s", label, count, phaseStyle(phase).Render(bar))
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// Broker health table.
	b.WriteString(styleHeader.Render("Runtime brokers"))
	b.WriteString("\n")
	header := strings.Join([]string{
		padCell("NAME", brokerColName),
		padCell("STATUS", brokerColStatus),
		padCell("CONNECTION", brokerColConn),
		padCell("LAST HEARTBEAT", brokerColSeen),
	}, "  ")
	b.WriteString(styleHeader.Render(header))
	b.WriteString("\n")
	if len(s.Brokers) == 0 {
		b.WriteString(styleDim.Render("  No runtime brokers found."))
		b.WriteString("\n")
	} else {
		for _, br := range s.Brokers {
			b.WriteString(renderBrokerRow(br))
			b.WriteString("\n")
		}
	}
}

// renderBrokerRow renders a single broker health row, matching the field
// conventions runHubBrokers uses (relative last-heartbeat time).
func renderBrokerRow(br BrokerHealth) string {
	return strings.Join([]string{
		padCell(br.Name, brokerColName),
		padCell(br.Status, brokerColStatus),
		padCell(br.ConnectionState, brokerColConn),
		padCell(formatRelativeTime(br.LastHeartbeat), brokerColSeen),
	}, "  ")
}

// hubStatsBar returns a proportional bar of block characters for count relative
// to maxCount, capped at hubStatsBarWidth. A non-zero count always renders at
// least one block so small-but-present phases stay visible.
func hubStatsBar(count, maxCount int) string {
	if count <= 0 || maxCount <= 0 {
		return ""
	}
	width := count * hubStatsBarWidth / maxCount
	if width < 1 {
		width = 1
	}
	return strings.Repeat("█", width)
}
