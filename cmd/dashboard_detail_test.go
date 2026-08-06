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
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// modelWithDetail returns a test model with the detail pane open on a loaded
// capture for the "alpha" agent, bypassing the real capture-pane exec so state
// transitions are deterministic.
func modelWithDetail() dashboardModel {
	m := newTestModel()
	m.showDetail = true
	m.detailName = "alpha"
	m.detailSlug = "alpha"
	m.detailLoaded = true
	m.detailOutput = "$ echo hello\nhello\n"
	return m
}

func TestDashboardEnterOpensDetail(t *testing.T) {
	m := newTestModel()
	if m.showDetail {
		t.Fatal("detail pane should start closed")
	}
	// Selection starts on the first row (alpha).
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(dashboardModel)

	if !m.showDetail {
		t.Error("pressing Enter should open the detail pane")
	}
	if m.detailName != "alpha" || m.detailSlug != "alpha" {
		t.Errorf("detail should target the selected agent: name=%q slug=%q, want alpha/alpha", m.detailName, m.detailSlug)
	}
	if m.detailLoaded {
		t.Error("detail should not be marked loaded before the first capture returns")
	}
	if cmd == nil {
		t.Error("opening the detail pane must return a capture cmd to fetch the first frame")
	}
}

func TestDashboardEnterNoOpWhenEmpty(t *testing.T) {
	m := newDashboardModel(nil, 5*time.Second)
	m.loading = false
	// No agents fetched -> nothing selectable.
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(dashboardModel)
	if m.showDetail {
		t.Error("Enter with no selectable row should not open the detail pane")
	}
	if cmd != nil {
		t.Error("Enter with no selectable row should not issue a capture cmd")
	}
}

func TestDashboardDetailEscReturnsToList(t *testing.T) {
	m := modelWithDetail()
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(dashboardModel)

	if m.showDetail {
		t.Error("esc should close the detail pane")
	}
	if m.detailOutput != "" || m.detailSlug != "" || m.detailLoaded {
		t.Errorf("esc should reset detail state: output=%q slug=%q loaded=%v", m.detailOutput, m.detailSlug, m.detailLoaded)
	}
	// The list view should render again after returning.
	if !strings.Contains(m.View().Content, "NAME") {
		t.Error("after esc the agent list should render again")
	}
}

func TestDashboardDetailQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		keyPress("q"),
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		m := modelWithDetail()
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %q with detail open returned nil cmd, want tea.Quit", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("key %q with detail open did not produce QuitMsg", key.String())
		}
	}
}

func TestDashboardDetailViewRenders(t *testing.T) {
	m := modelWithDetail()
	out := m.View().Content
	for _, want := range []string{"scion look", "alpha", "hello", "esc back", "q quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail View() missing %q\n%s", want, out)
		}
	}
}

func TestDashboardDetailViewLoadingState(t *testing.T) {
	m := newTestModel()
	m.showDetail = true
	m.detailName = "alpha"
	m.detailSlug = "alpha"
	m.detailLoaded = false
	if !strings.Contains(m.View().Content, "Capturing terminal output") {
		t.Errorf("unloaded detail View() should show a capturing message, got:\n%s", m.View().Content)
	}
}

func TestDashboardLookMsgPopulatesPane(t *testing.T) {
	m := newTestModel()
	m.showDetail = true
	m.detailSlug = "alpha"
	m.detailLoaded = false

	mi, _ := m.Update(lookMsg{agent: "alpha", output: "captured output"})
	m = mi.(dashboardModel)

	if !m.detailLoaded {
		t.Error("lookMsg should mark the pane loaded")
	}
	if m.detailOutput != "captured output" {
		t.Errorf("detailOutput = %q, want %q", m.detailOutput, "captured output")
	}
	if m.detailErr != nil {
		t.Errorf("detailErr should be nil on success, got %v", m.detailErr)
	}
}

func TestDashboardLookMsgError(t *testing.T) {
	m := newTestModel()
	m.showDetail = true
	m.detailSlug = "alpha"

	wantErr := errors.New("capture failed")
	mi, _ := m.Update(lookMsg{agent: "alpha", err: wantErr})
	m = mi.(dashboardModel)

	if !m.detailLoaded {
		t.Error("lookMsg with error should still mark the pane loaded")
	}
	if m.detailErr == nil {
		t.Fatal("lookMsg error should be recorded on the model")
	}
	if !strings.Contains(m.View().Content, "capture failed") {
		t.Errorf("detail View() should surface the capture error, got:\n%s", m.View().Content)
	}
}

func TestDashboardLookMsgStaleIgnored(t *testing.T) {
	m := newTestModel()
	m.showDetail = true
	m.detailSlug = "alpha"
	m.detailOutput = "current"
	m.detailLoaded = true

	// A result for a different (previously-viewed) agent must not overwrite the
	// pane the user is now looking at.
	mi, _ := m.Update(lookMsg{agent: "bravo", output: "stale output"})
	m = mi.(dashboardModel)

	if m.detailOutput != "current" {
		t.Errorf("stale lookMsg should be ignored, detailOutput = %q, want %q", m.detailOutput, "current")
	}
}

func TestDashboardLookMsgIgnoredWhenClosed(t *testing.T) {
	m := newTestModel()
	m.showDetail = false

	mi, _ := m.Update(lookMsg{agent: "alpha", output: "late output"})
	m = mi.(dashboardModel)

	if m.detailOutput != "" {
		t.Errorf("lookMsg arriving after the pane closed should be ignored, got %q", m.detailOutput)
	}
}
