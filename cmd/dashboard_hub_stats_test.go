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
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func sampleHubStats() HubStats {
	return HubStats{
		ProjectCount:  2,
		AgentCount:    4,
		AgentsByPhase: map[string]int{"running": 2, "stopped": 1, "unknown": 1},
		Brokers: []BrokerHealth{
			{Name: "broker-1", Status: "online", ConnectionState: "connected", LastHeartbeat: time.Now()},
			{Name: "broker-2", Status: "offline", ConnectionState: "disconnected"},
		},
	}
}

// modelWithHubStats returns a test model in Hub mode with the Hub-stats panel
// open and a loaded snapshot.
func modelWithHubStats() dashboardModel {
	m := newTestModel()
	m.hubCtx = &HubContext{Endpoint: "https://hub.example"}
	m.showHubStats = true
	m.hubStatsLoaded = true
	m.hubStats = sampleHubStats()
	return m
}

func TestDashboardOpenHubStatsHubMode(t *testing.T) {
	m := newTestModel()
	m.hubCtx = &HubContext{Endpoint: "https://hub.example"}
	if m.showHubStats {
		t.Fatal("panel should start closed")
	}
	mi, cmd := m.Update(keyPress("h"))
	m = mi.(dashboardModel)
	if !m.showHubStats {
		t.Error("pressing 'h' should open the Hub-stats panel")
	}
	if m.hubStatsErr != nil {
		t.Errorf("Hub-mode open should not set an error, got %v", m.hubStatsErr)
	}
	if cmd == nil {
		t.Error("Hub-mode open should return a fetch cmd")
	}
}

func TestDashboardOpenHubStatsLocalMode(t *testing.T) {
	m := newTestModel() // local mode: hubCtx == nil
	mi, cmd := m.Update(keyPress("h"))
	m = mi.(dashboardModel)
	if !m.showHubStats {
		t.Error("pressing 'h' should open the panel even in local mode")
	}
	if m.hubStatsErr == nil {
		t.Error("local mode should record the requires-Hub message")
	}
	if cmd != nil {
		t.Error("local mode must not trigger a fetch cmd")
	}
	out := m.View().Content
	if !strings.Contains(out, "Hub mode") {
		t.Errorf("local-mode panel should explain it requires Hub mode, got:\n%s", out)
	}
}

func TestDashboardHubStatsViewRenders(t *testing.T) {
	m := modelWithHubStats()
	out := m.View().Content
	for _, want := range []string{
		"Hub stats", "Projects: 2", "Agents: 4", "Brokers: 2",
		"Agents by phase", "running", "stopped", "unknown",
		"Runtime brokers", "NAME", "STATUS", "CONNECTION", "LAST HEARTBEAT",
		"broker-1", "broker-2", "connected",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hub-stats View() missing %q\n%s", want, out)
		}
	}
}

func TestDashboardHubStatsLoadingState(t *testing.T) {
	m := newTestModel()
	m.hubCtx = &HubContext{Endpoint: "https://hub.example"}
	m.showHubStats = true
	m.hubStatsLoaded = false
	out := m.View().Content
	if !strings.Contains(out, "Loading hub stats") {
		t.Errorf("unloaded panel should show a loading message, got:\n%s", out)
	}
}

func TestDashboardHubStatsMsgUpdatesModel(t *testing.T) {
	m := newTestModel()
	m.hubCtx = &HubContext{}
	m.showHubStats = true

	mi, _ := m.Update(hubStatsMsg{stats: sampleHubStats()})
	m = mi.(dashboardModel)
	if !m.hubStatsLoaded {
		t.Error("hubStatsMsg should mark stats loaded")
	}
	if m.hubStatsErr != nil {
		t.Errorf("successful hubStatsMsg should clear error, got %v", m.hubStatsErr)
	}
	if m.hubStats.ProjectCount != 2 {
		t.Errorf("hubStatsMsg should populate stats, ProjectCount = %d", m.hubStats.ProjectCount)
	}
}

func TestDashboardHubStatsEscReturns(t *testing.T) {
	m := modelWithHubStats()
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(dashboardModel)
	if m.showHubStats {
		t.Error("esc should close the Hub-stats panel")
	}
	// The agent list should render again after returning.
	out := m.View().Content
	if !strings.Contains(out, "TEMPLATE") {
		t.Errorf("after esc, the agent list should render, got:\n%s", out)
	}
}

func TestDashboardHubStatsQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		keyPress("q"),
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		m := modelWithHubStats()
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %q with panel open returned nil cmd, want tea.Quit", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q with panel open did not produce QuitMsg", key.String())
		}
	}
}

func TestHubStatsBar(t *testing.T) {
	if got := hubStatsBar(0, 10); got != "" {
		t.Errorf("zero count should render empty bar, got %q", got)
	}
	if got := hubStatsBar(10, 10); len([]rune(got)) != hubStatsBarWidth {
		t.Errorf("max count should render full-width bar, got width %d", len([]rune(got)))
	}
	// A small-but-present count renders at least one block.
	if got := hubStatsBar(1, 1000); len([]rune(got)) < 1 {
		t.Errorf("non-zero count should render at least one block, got %q", got)
	}
}
