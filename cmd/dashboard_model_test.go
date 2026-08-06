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

	"github.com/GoogleCloudPlatform/scion/pkg/api"
)

func sampleAgents() []api.AgentInfo {
	return []api.AgentInfo{
		{Slug: "alpha", Name: "alpha", Template: "developer", Phase: "running", Activity: "executing"},
		{Slug: "bravo", Name: "bravo", Template: "eng-manager", Phase: "stopped", Activity: "completed"},
		{Slug: "charlie", Name: "charlie", Template: "coordinator", Phase: "error", Activity: "crashed"},
	}
}

func newTestModel() dashboardModel {
	m := newDashboardModel(nil, 5*time.Second)
	m.loading = false
	m.applyFetch(sampleAgents())
	return m
}

// keyPress builds a printable-character KeyPressMsg.
func keyPress(s string) tea.KeyPressMsg {
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestDashboardViewRendersAgents(t *testing.T) {
	m := newTestModel()
	out := m.View().Content

	for _, want := range []string{"NAME", "TEMPLATE", "PHASE", "ACTIVITY", "LAST ACTIVITY"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing header %q\n%s", want, out)
		}
	}
	for _, want := range []string{"alpha", "bravo", "charlie", "developer", "running"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing content %q\n%s", want, out)
		}
	}
}

func TestDashboardEmptyStateRenders(t *testing.T) {
	m := newDashboardModel(nil, 5*time.Second)
	m.loading = false
	// No agents fetched.
	out := m.View().Content
	if !strings.Contains(out, "No active agents") {
		t.Errorf("empty-state View() should mention no agents, got:\n%s", out)
	}
}

func TestDashboardQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		keyPress("q"),
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		m := newTestModel()
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %q returned nil cmd, want tea.Quit", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q did not produce QuitMsg", key.String())
		}
	}
}

func TestDashboardSelectionMovement(t *testing.T) {
	m := newTestModel()
	if m.selected != 0 {
		t.Fatalf("initial selection = %d, want 0", m.selected)
	}

	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(dashboardModel)
	if m.selected != 1 {
		t.Errorf("after down, selection = %d, want 1", m.selected)
	}

	mi, _ = m.Update(keyPress("j"))
	m = mi.(dashboardModel)
	if m.selected != 2 {
		t.Errorf("after j, selection = %d, want 2", m.selected)
	}

	// Clamp at the bottom.
	mi, _ = m.Update(keyPress("j"))
	m = mi.(dashboardModel)
	if m.selected != 2 {
		t.Errorf("selection should clamp at 2, got %d", m.selected)
	}

	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = mi.(dashboardModel)
	if m.selected != 1 {
		t.Errorf("after up, selection = %d, want 1", m.selected)
	}
}

func TestDashboardFilter(t *testing.T) {
	m := newTestModel()

	// Enter filter mode and type "alpha".
	mi, _ := m.Update(keyPress("/"))
	m = mi.(dashboardModel)
	if !m.filtering {
		t.Fatal("expected filtering mode after '/'")
	}
	for _, r := range "alpha" {
		mi, _ = m.Update(keyPress(string(r)))
		m = mi.(dashboardModel)
	}
	visible := m.visibleAgents()
	if len(visible) != 1 || visible[0].Name != "alpha" {
		t.Errorf("filter 'alpha' -> %d rows, want 1 (alpha)", len(visible))
	}

	// Enter applies the filter (leaves input mode, keeps query).
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(dashboardModel)
	if m.filtering {
		t.Error("enter should leave filtering mode")
	}
	if m.filter != "alpha" {
		t.Errorf("filter query = %q, want alpha", m.filter)
	}

	// Filter by template substring.
	m2 := newTestModel()
	m2.filter = "eng"
	v := m2.visibleAgents()
	if len(v) != 1 || v[0].Name != "bravo" {
		t.Errorf("filter 'eng' should match bravo, got %d rows", len(v))
	}
}

func TestDashboardFilterEscClears(t *testing.T) {
	m := newTestModel()
	m.filtering = true
	m.filter = "alpha"
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(dashboardModel)
	if m.filtering {
		t.Error("esc should leave filtering mode")
	}
	if m.filter != "" {
		t.Errorf("esc should clear filter, got %q", m.filter)
	}
	if len(m.visibleAgents()) != 3 {
		t.Errorf("after esc, all 3 agents should be visible, got %d", len(m.visibleAgents()))
	}
}

func TestDashboardSortCycle(t *testing.T) {
	m := newTestModel()
	if dashboardSortFields[m.sortIdx] != "" {
		t.Fatalf("initial sort = %q, want none", dashboardSortFields[m.sortIdx])
	}
	// Press 's' once -> "name" sort.
	mi, _ := m.Update(keyPress("s"))
	m = mi.(dashboardModel)
	if dashboardSortFields[m.sortIdx] != "name" {
		t.Errorf("after one 's', sort = %q, want name", dashboardSortFields[m.sortIdx])
	}
	v := m.visibleAgents()
	if v[0].Name != "alpha" || v[2].Name != "charlie" {
		t.Errorf("name sort order wrong: %s, %s, %s", v[0].Name, v[1].Name, v[2].Name)
	}
}

func TestDashboardSelectionStableAcrossRefresh(t *testing.T) {
	m := newTestModel()
	// Select "bravo".
	m.selected = 1
	if m.selectedSlug() != "bravo" {
		t.Fatalf("selected slug = %q, want bravo", m.selectedSlug())
	}
	// Refresh with reordered data; bravo moves to index 0.
	reordered := []api.AgentInfo{
		{Slug: "bravo", Name: "bravo", Template: "eng-manager", Phase: "stopped"},
		{Slug: "alpha", Name: "alpha", Template: "developer", Phase: "running"},
		{Slug: "charlie", Name: "charlie", Template: "coordinator", Phase: "error"},
	}
	m.applyFetch(reordered)
	if m.selectedSlug() != "bravo" {
		t.Errorf("selection not preserved by slug: got %q, want bravo", m.selectedSlug())
	}
	if m.selected != 0 {
		t.Errorf("bravo should now be at index 0, selected = %d", m.selected)
	}
}
