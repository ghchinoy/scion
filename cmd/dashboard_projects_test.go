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

	tea "charm.land/bubbletea/v2"

	"github.com/GoogleCloudPlatform/scion/pkg/config"
)

func sampleProjects() []config.ProjectInfo {
	return []config.ProjectInfo{
		{Name: "global", Type: config.ProjectTypeGlobal, Status: config.ProjectStatusOK, AgentCount: 2},
		{Name: "alpha-proj", Type: config.ProjectTypeGit, WorkspacePath: "/home/u/alpha", Status: config.ProjectStatusOK, AgentCount: 3},
		{Name: "beta-proj", Type: config.ProjectTypeExternal, WorkspacePath: "/home/u/beta", Status: config.ProjectStatusOrphaned, AgentCount: 0},
	}
}

// modelWithPicker returns a test model with the picker open and a fixed project
// list (bypassing real filesystem discovery so transitions are deterministic).
func modelWithPicker() dashboardModel {
	m := newTestModel()
	m.showProjects = true
	m.projects = sampleProjects()
	m.projectSel = 0
	return m
}

func TestDashboardOpenProjectsKey(t *testing.T) {
	m := newTestModel()
	if m.showProjects {
		t.Fatal("picker should start closed")
	}
	mi, _ := m.Update(keyPress("p"))
	m = mi.(dashboardModel)
	if !m.showProjects {
		t.Error("pressing 'p' should open the project picker")
	}
	// Discovery in this environment must find at least one project (the
	// scion/global project itself), or surface an error — never a silent
	// empty, panic-free state.
	if m.projectsErr == nil && len(m.projects) == 0 {
		t.Error("expected DiscoverProjects to find at least one project or return an error")
	}
}

func TestDashboardProjectPickerRenders(t *testing.T) {
	m := modelWithPicker()
	out := m.View().Content
	for _, want := range []string{"Select project", "NAME", "TYPE", "AGENTS", "STATUS", "alpha-proj", "beta-proj", "global"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker View() missing %q\n%s", want, out)
		}
	}
}

func TestDashboardProjectPickerNavigation(t *testing.T) {
	m := modelWithPicker()

	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(dashboardModel)
	if m.projectSel != 1 {
		t.Errorf("after down, projectSel = %d, want 1", m.projectSel)
	}

	mi, _ = m.Update(keyPress("j"))
	m = mi.(dashboardModel)
	if m.projectSel != 2 {
		t.Errorf("after j, projectSel = %d, want 2", m.projectSel)
	}

	// Clamp at the bottom.
	mi, _ = m.Update(keyPress("j"))
	m = mi.(dashboardModel)
	if m.projectSel != 2 {
		t.Errorf("projectSel should clamp at 2, got %d", m.projectSel)
	}

	mi, _ = m.Update(keyPress("k"))
	m = mi.(dashboardModel)
	if m.projectSel != 1 {
		t.Errorf("after k, projectSel = %d, want 1", m.projectSel)
	}
}

func TestDashboardProjectPickerEscCloses(t *testing.T) {
	saved := projectPath
	defer func() { projectPath = saved }()

	m := modelWithPicker()
	m.scopePath = ""
	m.scopeName = "current"
	m.projectSel = 1 // highlight alpha-proj, but esc must not commit it

	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(dashboardModel)

	if m.showProjects {
		t.Error("esc should close the picker")
	}
	if m.scopePath != "" || m.scopeName != "current" {
		t.Errorf("esc must not change scope: got scopePath=%q scopeName=%q", m.scopePath, m.scopeName)
	}
}

func TestDashboardProjectSelectRescopes(t *testing.T) {
	saved := projectPath
	defer func() { projectPath = saved }()

	m := modelWithPicker()
	m.projectSel = 1 // alpha-proj -> /home/u/alpha

	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(dashboardModel)

	if m.showProjects {
		t.Error("enter should close the picker")
	}
	if m.scopePath != "/home/u/alpha" {
		t.Errorf("scopePath = %q, want /home/u/alpha", m.scopePath)
	}
	if m.scopeName != "alpha-proj" {
		t.Errorf("scopeName = %q, want alpha-proj", m.scopeName)
	}
	if projectPath != "/home/u/alpha" {
		t.Errorf("package projectPath = %q, want /home/u/alpha (re-scope must re-parameterize the shared fetch path)", projectPath)
	}
	if cmd == nil {
		t.Error("selecting a project must return a fetch cmd to re-trigger the agent list")
	}
}

func TestDashboardProjectSelectGlobalScope(t *testing.T) {
	saved := projectPath
	defer func() { projectPath = saved }()

	m := modelWithPicker()
	m.projectSel = 0 // global project

	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(dashboardModel)

	if m.scopePath != "global" {
		t.Errorf("global project should scope to the \"global\" sentinel, got %q", m.scopePath)
	}
	if projectPath != "global" {
		t.Errorf("package projectPath = %q, want global", projectPath)
	}
}

func TestDashboardPickerQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		keyPress("q"),
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		m := modelWithPicker()
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %q with picker open returned nil cmd, want tea.Quit", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q with picker open did not produce QuitMsg", key.String())
		}
	}
}

func TestRescopeMsgUpdatesModel(t *testing.T) {
	m := modelWithPicker()
	m.showProjects = false
	m.loading = true

	newAgents := sampleAgents()
	mi, _ := m.Update(rescopeMsg{hubCtx: nil, agents: newAgents})
	m = mi.(dashboardModel)

	if m.loading {
		t.Error("rescopeMsg should clear loading")
	}
	if len(m.visibleAgents()) != len(newAgents) {
		t.Errorf("rescopeMsg should populate agents: got %d, want %d", len(m.visibleAgents()), len(newAgents))
	}
	if m.selected != 0 {
		t.Errorf("selection should reset to 0 on re-scope, got %d", m.selected)
	}
}

func TestResolveScopeName(t *testing.T) {
	cases := map[string]string{
		"":              "current",
		"global":        "global",
		"/home/u/alpha": "alpha",
	}
	for in, want := range cases {
		if got := resolveScopeName(in); got != want {
			t.Errorf("resolveScopeName(%q) = %q, want %q", in, got, want)
		}
	}
}
