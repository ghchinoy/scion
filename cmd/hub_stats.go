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
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/hubclient"
)

// errHubStatsRequiresHub is surfaced when the Hub-stats screen is opened while
// the dashboard is running in local mode. There is no local/non-Hub fleet-wide
// view (see cmd/list.go local mode), so this is a Hub-mode-only screen by
// nature.
var errHubStatsRequiresHub = errors.New("Hub stats require Hub mode (this dashboard is running in local mode)")

// BrokerHealth is the per-broker health slice of a HubStats snapshot. It mirrors
// the fields runHubBrokers renders today (cmd/hub.go) without pulling in the
// rest of hubclient.RuntimeBroker.
type BrokerHealth struct {
	Name            string
	Status          string
	ConnectionState string
	LastHeartbeat   time.Time
}

// HubStats is a plain, already-computed snapshot of fleet-wide Hub statistics.
// It carries no rendering logic: fetchHubStats populates it and the dashboard
// Hub-stats view (dashboard_hub_stats.go) decides how to present it, mirroring
// the fetchAgents*/display split shared with the `list` command.
type HubStats struct {
	ProjectCount int // total projects visible to the Hub session
	AgentCount   int // total agents across all projects

	// AgentsByPhase tallies agents by lifecycle phase across the whole fleet.
	// Agents with an empty phase are counted under "unknown".
	AgentsByPhase map[string]int

	// Brokers is the runtime-broker health list (Name/Status/ConnectionState/
	// LastHeartbeat), in the order returned by the Hub.
	Brokers []BrokerHealth
}

// hubStatsClient is the narrow slice of hubclient.Client that fetchHubStats
// needs. hubclient.Client satisfies it directly; tests supply a fake that only
// implements these three accessors, without stubbing the whole client surface.
type hubStatsClient interface {
	Projects() hubclient.ProjectService
	Agents() hubclient.AgentService
	RuntimeBrokers() hubclient.RuntimeBrokerService
}

// fetchHubStats gathers fleet-wide Hub statistics via three existing hubclient
// calls (no new backend endpoints, no admin role required) and returns a plain
// HubStats struct. It prints nothing — the data half of the Hub-stats screen,
// mirroring fetchAgentsViaHub. hubCtx must be non-nil (Hub mode); local mode
// returns errHubStatsRequiresHub.
func fetchHubStats(hubCtx *HubContext) (HubStats, error) {
	if hubCtx == nil || hubCtx.Client == nil {
		return HubStats{}, errHubStatsRequiresHub
	}
	return fetchHubStatsFromClient(hubCtx.Client)
}

// fetchHubStatsFromClient is the client-only core of fetchHubStats, split out so
// tests can drive it with a fake hubStatsClient (no live Hub required).
func fetchHubStatsFromClient(client hubStatsClient) (HubStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stats := HubStats{AgentsByPhase: map[string]int{}}

	// 1. Projects: count + (server-side populated) per-project totals. One call,
	//    no admin role required (GET /api/v1/projects).
	projResp, err := client.Projects().List(ctx, nil)
	if err != nil {
		return HubStats{}, wrapHubError(fmt.Errorf("failed to list projects via Hub: %w", err))
	}
	if projResp != nil {
		stats.ProjectCount = len(projResp.Projects)
	}

	// 2. Agents (unscoped — no ProjectID): every agent visible to the Hub
	//    session across all projects, tallied by phase client-side. Same
	//    unscoped call `scion list --all` uses.
	agentResp, err := client.Agents().List(ctx, &hubclient.ListAgentsOptions{})
	if err != nil {
		return HubStats{}, wrapHubError(fmt.Errorf("failed to list agents via Hub: %w", err))
	}
	if agentResp != nil {
		stats.AgentCount = len(agentResp.Agents)
		for _, a := range agentResp.Agents {
			phase := a.Phase
			if phase == "" {
				phase = "unknown"
			}
			stats.AgentsByPhase[phase]++
		}
	}

	// 3. Runtime brokers: health snapshot (GET /api/v1/runtime-brokers).
	brokerResp, err := client.RuntimeBrokers().List(ctx, nil)
	if err != nil {
		return HubStats{}, wrapHubError(fmt.Errorf("failed to list runtime brokers via Hub: %w", err))
	}
	if brokerResp != nil {
		stats.Brokers = make([]BrokerHealth, 0, len(brokerResp.Brokers))
		for _, br := range brokerResp.Brokers {
			stats.Brokers = append(stats.Brokers, BrokerHealth{
				Name:            br.Name,
				Status:          br.Status,
				ConnectionState: br.ConnectionState,
				LastHeartbeat:   br.LastHeartbeat,
			})
		}
	}

	return stats, nil
}
