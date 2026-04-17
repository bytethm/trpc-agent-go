//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package team

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	swarmHistoryRootRuntimeKey = "__swarm_history_root_filter_key__"
	swarmHistoryFilterMarker   = "__swarm__"
	swarmAgentOnlyFilterPrefix = "__swarm_agent__"
)

type swarmMemberRoute struct {
	teamName     string
	member       agent.Agent
	historyScope SwarmHistoryScope
}

func newSwarmRoutes(
	teamName string,
	members []agent.Agent,
	historyScope SwarmHistoryScope,
) map[string]agent.Agent {
	if len(members) == 0 {
		return nil
	}
	routes := make(map[string]agent.Agent, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		name := member.Info().Name
		routes[name] = &swarmMemberRoute{
			teamName:     teamName,
			member:       member,
			historyScope: historyScope,
		}
	}
	return routes
}

func (r *swarmMemberRoute) Info() agent.Info {
	if r == nil || r.member == nil {
		return agent.Info{}
	}
	return r.member.Info()
}

func (r *swarmMemberRoute) SubAgents() []agent.Agent {
	if r == nil || r.member == nil {
		return nil
	}
	return r.member.SubAgents()
}

func (r *swarmMemberRoute) FindSubAgent(name string) agent.Agent {
	if r == nil || r.member == nil {
		return nil
	}
	return r.member.FindSubAgent(name)
}

func (r *swarmMemberRoute) Tools() []tool.Tool {
	if r == nil || r.member == nil {
		return nil
	}
	return r.member.Tools()
}

func (r *swarmMemberRoute) PrepareInvocation(inv *agent.Invocation) {
	if r == nil || r.member == nil || inv == nil {
		return
	}
	ensureSwarmHistoryRoot(inv)
	agent.WithInvocationAgent(r.member)(inv)
	if r.historyScope == SwarmHistoryScopeShared {
		return
	}
	agent.WithInvocationEventFilterKey(
		swarmMemberFilterKey(
			inv,
			r.teamName,
			r.member.Info().Name,
			r.historyScope,
		),
	)(inv)
}

func (r *swarmMemberRoute) Run(
	ctx context.Context,
	inv *agent.Invocation,
) (<-chan *event.Event, error) {
	r.PrepareInvocation(inv)
	return r.member.Run(ctx, inv)
}

// ensureSwarmHistoryRoot records the Swarm's history root into the invocation's
// runtime state, using the filter key observed at Swarm entry.
//
// The recorded root is consumed by SwarmHistoryScopeRootAndAgent to construct
// per-member filter keys of the form {root}/__swarm__/{team}/{agent}, so
// prefix-based history filtering includes the enclosing context in addition
// to the member's own history.
//
// Note that "root" is not a global session root. When the Swarm is nested
// inside another agent (Coordinator, AgentTool, or another Swarm), root is
// the enclosing agent's filter key at the time this Swarm is entered. This
// scopes "shared context" for RootAndAgent members to whatever the Swarm's
// immediate parent branch saw, which is intentional but worth knowing when
// reasoning about visibility in nested topologies.
func ensureSwarmHistoryRoot(inv *agent.Invocation) {
	if inv == nil {
		return
	}
	if inv.RunOptions.RuntimeState == nil {
		inv.RunOptions.RuntimeState = make(map[string]any)
	}
	if _, ok := inv.RunOptions.RuntimeState[swarmHistoryRootRuntimeKey]; ok {
		return
	}
	inv.RunOptions.RuntimeState[swarmHistoryRootRuntimeKey] = inv.GetEventFilterKey()
}

func swarmHistoryRoot(inv *agent.Invocation) string {
	if inv == nil || inv.RunOptions.RuntimeState == nil {
		return ""
	}
	root, ok := inv.RunOptions.RuntimeState[swarmHistoryRootRuntimeKey].(string)
	if !ok {
		return ""
	}
	return root
}

func swarmMemberFilterKey(
	inv *agent.Invocation,
	teamName string,
	agentName string,
	scope SwarmHistoryScope,
) string {
	switch scope {
	case SwarmHistoryScopeRootAndAgent:
		return joinFilterKey(
			swarmHistoryRoot(inv),
			swarmHistoryFilterMarker,
			teamName,
			agentName,
		)
	case SwarmHistoryScopeAgentOnly:
		return swarmAgentOnlyFilterPrefix +
			"::" + teamName +
			"::" + agentName
	default:
		return inv.GetEventFilterKey()
	}
}

func joinFilterKey(parts ...string) string {
	var out string
	for _, part := range parts {
		if part == "" {
			continue
		}
		if out == "" {
			out = part
			continue
		}
		out += agent.EventFilterKeyDelimiter + part
	}
	return out
}
