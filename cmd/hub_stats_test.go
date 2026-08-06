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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/hubclient"
)

// --- test fakes ------------------------------------------------------------
//
// Each fake embeds the real hubclient service interface so it satisfies the
// interface with only List() implemented; the other methods are never called by
// fetchHubStatsFromClient and would panic (nil) if they were, which is the
// intended guard.

type fakeProjectService struct {
	hubclient.ProjectService
	resp *hubclient.ListProjectsResponse
	err  error
}

func (f fakeProjectService) List(context.Context, *hubclient.ListProjectsOptions) (*hubclient.ListProjectsResponse, error) {
	return f.resp, f.err
}

type fakeAgentService struct {
	hubclient.AgentService
	resp *hubclient.ListAgentsResponse
	err  error
}

func (f fakeAgentService) List(context.Context, *hubclient.ListAgentsOptions) (*hubclient.ListAgentsResponse, error) {
	return f.resp, f.err
}

type fakeBrokerService struct {
	hubclient.RuntimeBrokerService
	resp *hubclient.ListBrokersResponse
	err  error
}

func (f fakeBrokerService) List(context.Context, *hubclient.ListBrokersOptions) (*hubclient.ListBrokersResponse, error) {
	return f.resp, f.err
}

type fakeHubStatsClient struct {
	projects fakeProjectService
	agents   fakeAgentService
	brokers  fakeBrokerService
}

func (f fakeHubStatsClient) Projects() hubclient.ProjectService             { return f.projects }
func (f fakeHubStatsClient) Agents() hubclient.AgentService                 { return f.agents }
func (f fakeHubStatsClient) RuntimeBrokers() hubclient.RuntimeBrokerService { return f.brokers }

func sampleHubStatsClient() fakeHubStatsClient {
	return fakeHubStatsClient{
		projects: fakeProjectService{resp: &hubclient.ListProjectsResponse{
			Projects: []hubclient.Project{{Name: "alpha"}, {Name: "beta"}},
		}},
		agents: fakeAgentService{resp: &hubclient.ListAgentsResponse{
			Agents: []hubclient.Agent{
				{Phase: "running"},
				{Phase: "running"},
				{Phase: "stopped"},
				{Phase: ""}, // -> "unknown"
			},
		}},
		brokers: fakeBrokerService{resp: &hubclient.ListBrokersResponse{
			Brokers: []hubclient.RuntimeBroker{
				{Name: "broker-1", Status: "online", ConnectionState: "connected", LastHeartbeat: time.Now()},
				{Name: "broker-2", Status: "offline", ConnectionState: "disconnected"},
			},
		}},
	}
}

// --- tests -----------------------------------------------------------------

func TestFetchHubStatsFromClient(t *testing.T) {
	stats, err := fetchHubStatsFromClient(sampleHubStatsClient())
	if err != nil {
		t.Fatalf("fetchHubStatsFromClient returned error: %v", err)
	}

	if stats.ProjectCount != 2 {
		t.Errorf("ProjectCount = %d, want 2", stats.ProjectCount)
	}
	if stats.AgentCount != 4 {
		t.Errorf("AgentCount = %d, want 4", stats.AgentCount)
	}
	wantPhase := map[string]int{"running": 2, "stopped": 1, "unknown": 1}
	for phase, want := range wantPhase {
		if got := stats.AgentsByPhase[phase]; got != want {
			t.Errorf("AgentsByPhase[%q] = %d, want %d", phase, got, want)
		}
	}
	if len(stats.AgentsByPhase) != len(wantPhase) {
		t.Errorf("AgentsByPhase has %d keys, want %d: %v", len(stats.AgentsByPhase), len(wantPhase), stats.AgentsByPhase)
	}
	if len(stats.Brokers) != 2 {
		t.Fatalf("Brokers len = %d, want 2", len(stats.Brokers))
	}
	if stats.Brokers[0].Name != "broker-1" || stats.Brokers[0].ConnectionState != "connected" {
		t.Errorf("broker[0] = %+v, want broker-1/connected", stats.Brokers[0])
	}
}

func TestFetchHubStatsPropagatesErrors(t *testing.T) {
	sentinel := errors.New("boom")

	cases := map[string]fakeHubStatsClient{
		"projects": {
			projects: fakeProjectService{err: sentinel},
		},
		"agents": {
			projects: fakeProjectService{resp: &hubclient.ListProjectsResponse{}},
			agents:   fakeAgentService{err: sentinel},
		},
		"brokers": {
			projects: fakeProjectService{resp: &hubclient.ListProjectsResponse{}},
			agents:   fakeAgentService{resp: &hubclient.ListAgentsResponse{}},
			brokers:  fakeBrokerService{err: sentinel},
		},
	}

	for name, client := range cases {
		if _, err := fetchHubStatsFromClient(client); err == nil {
			t.Errorf("%s error not propagated", name)
		}
	}
}

func TestFetchHubStatsRequiresHub(t *testing.T) {
	if _, err := fetchHubStats(nil); !errors.Is(err, errHubStatsRequiresHub) {
		t.Errorf("fetchHubStats(nil) err = %v, want errHubStatsRequiresHub", err)
	}
	if _, err := fetchHubStats(&HubContext{Client: nil}); !errors.Is(err, errHubStatsRequiresHub) {
		t.Errorf("fetchHubStats(nil client) err = %v, want errHubStatsRequiresHub", err)
	}
}
