//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package skill

import (
	"sort"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/session"
)

// EnvDebugSkillState is kept for compatibility with temporary debugging docs.
const EnvDebugSkillState = "TRPC_AGENT_DEBUG_SKILL_STATE"

// DebugSkillStateEnabled reports whether temporary skill-state logs are enabled.
// Temporary diagnostics are currently always enabled.
func DebugSkillStateEnabled() bool {
	return true
}

// DebugSkillStateKeys returns sorted loaded/docs state keys with non-empty values.
func DebugSkillStateKeys(state session.StateMap) []string {
	if len(state) == 0 {
		return nil
	}
	out := make([]string, 0, len(state))
	for k, v := range state {
		if !strings.HasPrefix(k, StateKeyLoadedPrefix) &&
			!strings.HasPrefix(k, StateKeyDocsPrefix) &&
			!strings.HasPrefix(k, StateKeyLoadedByAgentPrefix) &&
			!strings.HasPrefix(k, StateKeyDocsByAgentPrefix) {
			continue
		}
		if len(v) == 0 {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DebugLoadedSkills returns sorted loaded skill names for the provided agent.
func DebugLoadedSkills(
	state session.StateMap,
	agentName string,
) []string {
	if len(state) == 0 {
		return nil
	}
	prefix := LoadedPrefix(agentName)
	out := make([]string, 0, len(state))
	for k, v := range state {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if len(v) == 0 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(k, prefix))
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// DebugStateDeltaKeys returns sorted delta keys, including nil-value clears.
func DebugStateDeltaKeys(delta map[string][]byte) []string {
	if len(delta) == 0 {
		return nil
	}
	out := make([]string, 0, len(delta))
	for k := range delta {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
