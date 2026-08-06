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
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
)

// dashboardInterval is the refresh interval for the dashboard TUI. When the
// user does not pass --interval, a mode-dependent default is used instead
// (5s local, 10s Hub).
var dashboardInterval time.Duration

const (
	dashboardDefaultLocalInterval = 5 * time.Second
	dashboardDefaultHubInterval   = 10 * time.Second
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Live, auto-refreshing view of agents",
	Long: `Open a full-screen, auto-refreshing terminal view of the current
project's agents.

The dashboard shows the same agents as 'scion list' (Name, Template, Phase,
Activity, Last Activity) and refreshes automatically. It is a read-only view:
use the arrow keys or j/k to move the selection, '/' to filter, 's' to cycle
the sort field, 'p' to switch the active project, 'h' for Hub stats, Enter to
open a detail pane, and 'q' or Ctrl+C to quit.

Press Enter on a selected agent to open the look/detail pane: a read-only,
live-updating view of that agent's current terminal output (the same capture
'scion look <agent>' shows), refreshed on the dashboard's interval. Esc returns
to the agent list. It forwards no input to the agent's session.

Press 'p' to open the project switcher: a picker listing the projects found by
'scion project list'. Selecting one re-scopes the agent list to that project;
Esc closes the picker without changing scope.

Press 'h' to open the Hub-stats panel (Hub mode only): a single-pane summary of
fleet-wide project, agent-by-phase, and runtime-broker health totals. Esc
returns to the agent list.

This command requires an interactive terminal; it is not available in
--non-interactive or agent mode. It is a terminal companion to the web UI,
not a replacement for it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Belt-and-suspenders guard on top of the gates in output.go
		// (--format json rejection) and cli_mode.go (agent-mode removal):
		// a full-screen TUI cannot run without an interactive terminal, and
		// --non-interactive can be passed by a human too.
		if IsNonInteractive() || resolveMode() == ModeAgent {
			return fmt.Errorf("scion dashboard requires an interactive terminal; it is not available with --non-interactive or in agent mode")
		}

		// Resolve Hub-vs-local mode once, up front, so the refresh loop does
		// not re-resolve it on every tick.
		hubCtx, err := CheckHubAvailability(projectPath)
		if err != nil {
			return err
		}

		// Pick a sensible default interval based on mode unless the user set
		// one explicitly. Hub calls cross the network, so they poll slower.
		interval := dashboardInterval
		if !cmd.Flags().Changed("interval") {
			if hubCtx != nil {
				interval = dashboardDefaultHubInterval
			} else {
				interval = dashboardDefaultLocalInterval
			}
		}
		if interval <= 0 {
			return fmt.Errorf("--interval must be a positive duration, got %s", interval)
		}

		program := tea.NewProgram(newDashboardModel(hubCtx, interval))
		_, err = program.Run()
		return err
	},
}

func init() {
	dashboardCmd.Flags().DurationVar(&dashboardInterval, "interval", dashboardDefaultLocalInterval,
		"Refresh interval (default 5s local, 10s Hub)")
	rootCmd.AddCommand(dashboardCmd)
}
