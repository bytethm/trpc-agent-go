//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package team

import "time"

// SwarmHistoryScope controls which history a Swarm member can see.
//
// A scope only shapes how Swarm derives event filter keys for its members at
// entry and transfer boundaries; it does not override per-agent timeline
// filters configured via llmagent.WithMessageFilterMode. If a member agent
// explicitly sets IsolatedInvocation (or any TimelineFilterCurrentInvocation
// / CurrentRequest mode), that configuration still applies on top and may
// further restrict what the member sees — including its own prior turns.
// In other words, Swarm manages the default Transfer/Swarm branch semantics
// but stays out of the way when a member has opted into a stricter timeline
// filter of its own.
type SwarmHistoryScope int

const (
	// SwarmHistoryScopeShared keeps the legacy behavior: all members share the
	// same filter key and therefore see the same Swarm history. Convenient for
	// simple baton-passing scenarios where context bleed between members is
	// acceptable.
	SwarmHistoryScopeShared SwarmHistoryScope = iota

	// SwarmHistoryScopeRootAndAgent gives each member a stable per-agent filter
	// key nested under the Swarm root. With the default prefix history filter,
	// the member can see shared root/user context plus its own prior history,
	// but not sibling members' history.
	//
	// "Root" here is the filter key observed at the moment the Swarm begins
	// executing. When the Swarm is nested inside another agent (Coordinator,
	// AgentTool, or another Swarm), root is the enclosing agent's filter key
	// at entry — it is not a global session root. When the Swarm is not
	// nested, root equals the session-level filter key set by the runner.
	//
	// Session summaries are scoped per-member: each member accumulates its
	// own summary under its member filter key, and under BranchFilterModePrefix
	// the content processor aggregates summaries that share the member's
	// prefix. There is no team-level shared summary — sibling members'
	// summaries are not merged in.
	SwarmHistoryScopeRootAndAgent

	// SwarmHistoryScopeAgentOnly gives each member a stable standalone filter
	// key that does not share a prefix with the Swarm root. The member keeps
	// its own prior history across transfers and across requests, but does not
	// inherit any prior shared/root history from the Swarm.
	//
	// Consequence for multi-turn sessions: user turns written under the Swarm
	// root (including user turns of previous requests) are NOT visible to the
	// member. The only message guaranteed to reach the member is the current
	// invocation's input message, preserved via the strict-invocation bypass
	// in the content processor. What that input actually is depends on how
	// the member was entered:
	//
	//   - Entry member: the originating user message.
	//   - Handoff target with a non-empty transferInfo.Message: the transfer
	//     message supplied by the source. The transfer processor overwrites
	//     invocation.Message only in this case.
	//   - Handoff target without an explicit transfer message (the message
	//     field in the transfer tool schema is optional): the message
	//     inherited from the source invocation, which may itself be an
	//     earlier user message or an earlier transfer message further up
	//     the handoff chain.
	//
	// Therefore, combining this scope with crossRequestTransfer means the
	// active member sees its own prior turns but not the user's prior
	// questions; and after a handoff the target's visible input is whatever
	// the source chose to pass (or implicitly inherited), not necessarily
	// the originating user request. Plan accordingly — for example, have
	// sources always supply an explicit, self-contained transfer message
	// when AgentOnly is in effect.
	SwarmHistoryScopeAgentOnly
)

// SwarmConfig defines optional safety limits for swarm-style handoffs.
//
// All fields are optional. A zero value means "no limit" for that field.
type SwarmConfig struct {
	// HistoryScope controls how Swarm members isolate or share history.
	HistoryScope SwarmHistoryScope

	// MaxHandoffs limits how many transfers can happen in a single run.
	MaxHandoffs int

	// NodeTimeout limits how long a single member agent may run after a
	// transfer. A zero value means no per-node timeout.
	NodeTimeout time.Duration

	// RepetitiveHandoffWindow is the sliding window size used to detect
	// repetitive handoff loops. A zero value disables this check.
	RepetitiveHandoffWindow int

	// RepetitiveHandoffMinUnique is the minimum number of unique agents that
	// must appear in the window. If fewer appear, the transfer is rejehui 
	// A zero value disables this check.
	RepetitiveHandoffMinUnique int
}

// DefaultSwarmConfig returns conservative defaults that prevent unbounded
// transfer loops while keeping behavior predictable.
func DefaultSwarmConfig() SwarmConfig {
	return SwarmConfig{
		HistoryScope:               SwarmHistoryScopeShared,
		MaxHandoffs:                20,
		RepetitiveHandoffWindow:    8,
		RepetitiveHandoffMinUnique: 3,
	}
}
