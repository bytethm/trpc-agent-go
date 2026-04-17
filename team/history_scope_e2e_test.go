//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package team

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	transfertool "trpc.group/trpc-go/trpc-agent-go/tool/transfer"
)

// requestContainsUserText returns true if any non-system message in the
// request contains the given substring.
func requestContainsUserText(req *model.Request, text string) bool {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem {
			continue
		}
		if strings.Contains(msg.Content, text) {
			return true
		}
	}
	return false
}

// newStaticAssistant returns a respond func producing a single assistant
// message with no tool calls.
func newStaticAssistant(content string) func(int, *model.Request) *model.Response {
	return func(_ int, _ *model.Request) *model.Response {
		return &model.Response{
			Done: true,
			Choices: []model.Choice{{
				Message: model.Message{Role: model.RoleAssistant, Content: content},
			}},
		}
	}
}

// newTransferringAssistant returns a respond func producing a single
// assistant message that issues a transfer_to_agent tool call with the
// provided target and message payload.
func newTransferringAssistant(content, targetAgent, transferMessage string) func(int, *model.Request) *model.Response {
	return func(_ int, _ *model.Request) *model.Response {
		return &model.Response{
			Done: true,
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: content,
					ToolCalls: []model.ToolCall{{
						ID: "tc-transfer",
						Function: model.FunctionDefinitionParam{
							Name:      transfertool.TransferToolName,
							Arguments: buildTransferArgs(targetAgent, transferMessage),
						},
					}},
				},
			}},
		}
	}
}

// TestSwarm_HistoryScopeRootAndAgent_E2E_IsolatesSiblingButPreservesRoot
// verifies that with SwarmHistoryScopeRootAndAgent:
//   - a handoff target CAN see the originating user message (via root prefix)
//   - a handoff target CANNOT see the sibling agent's assistant output
func TestSwarm_HistoryScopeRootAndAgent_E2E_IsolatesSiblingButPreservesRoot(t *testing.T) {
	const (
		agentAName  = "agent_a"
		agentBName  = "agent_b"
		tmName      = "root-and-agent-team"
		userMsg     = "What do you think about Go?"
		transferMsg = "please analyze"
		agentAText  = "Agent A thinks this is great! Let me transfer to agent B for analysis."
	)

	modelA := &isolationCaptureModel{
		name:    "model-a",
		respond: newTransferringAssistant(agentAText, agentBName, transferMsg),
	}
	modelB := &isolationCaptureModel{
		name:    "model-b",
		respond: newStaticAssistant("Agent B analysis complete."),
	}

	genConfig := model.GenerationConfig{MaxTokens: intPtrIso(500), Stream: false}
	agentA := llmagent.New(
		agentAName,
		llmagent.WithModel(modelA),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithDescription("Agent A"),
		llmagent.WithInstruction("You are agent A."),
	)
	agentB := llmagent.New(
		agentBName,
		llmagent.WithModel(modelB),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithDescription("Agent B"),
		llmagent.WithInstruction("You are agent B."),
	)

	cfg := DefaultSwarmConfig()
	cfg.HistoryScope = SwarmHistoryScopeRootAndAgent
	tm, err := NewSwarm(tmName, agentAName,
		[]agent.Agent{agentA, agentB},
		WithSwarmConfig(cfg),
	)
	require.NoError(t, err)

	r := runner.NewRunner("test-app", tm,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	defer r.Close()

	ch, err := r.Run(context.Background(), "u", "s-root-agent", model.NewUserMessage(userMsg))
	require.NoError(t, err)
	for evt := range ch {
		if evt != nil && evt.Error != nil {
			t.Logf("evt error from %s: %s", evt.Author, evt.Error.Message)
		}
	}

	bReqs := modelB.getRequests()
	require.NotEmpty(t, bReqs, "agent_b should have been called")
	firstReq := bReqs[0]
	dumpRequestMessages(t, "agent_b (RootAndAgent)", firstReq)

	require.False(t, requestContainsAgentAContent(firstReq),
		"RootAndAgent: agent_b must NOT see agent_a's sibling output")

	require.True(t, requestContainsUserText(firstReq, userMsg),
		"RootAndAgent: agent_b must see the originating user message via root prefix")
}

// TestSwarm_HistoryScopeAgentOnly_E2E_HandoffInputMessageVariants verifies
// the three cases documented on SwarmHistoryScopeAgentOnly for what the
// member's current invocation input message actually is:
//
//  1. Entry member: originating user message.
//  2. Handoff target with non-empty transferInfo.Message: the transfer
//     message.
//  3. Handoff target with empty transferInfo.Message: the message inherited
//     from the source invocation (here, the originating user message).
func TestSwarm_HistoryScopeAgentOnly_E2E_HandoffInputMessageVariants(t *testing.T) {
	const (
		agentAName = "agent_a"
		agentBName = "agent_b"
		tmName     = "agent-only-team"
		userMsg    = "What do you think about Go?"
	)

	t.Run("entry_member_sees_user_message", func(t *testing.T) {
		modelA := &isolationCaptureModel{
			name:    "model-a-entry",
			respond: newStaticAssistant("Agent A answers directly, no transfer."),
		}
		modelB := &isolationCaptureModel{name: "model-b-entry"}

		agentA := llmagent.New(agentAName,
			llmagent.WithModel(modelA),
			llmagent.WithInstruction("You are agent A."),
		)
		agentB := llmagent.New(agentBName,
			llmagent.WithModel(modelB),
			llmagent.WithInstruction("You are agent B."),
		)

		cfg := DefaultSwarmConfig()
		cfg.HistoryScope = SwarmHistoryScopeAgentOnly
		tm, err := NewSwarm(tmName, agentAName,
			[]agent.Agent{agentA, agentB},
			WithSwarmConfig(cfg),
		)
		require.NoError(t, err)

		r := runner.NewRunner("test-app", tm,
			runner.WithSessionService(sessioninmemory.NewSessionService()),
		)
		defer r.Close()

		ch, err := r.Run(context.Background(), "u", "s-entry",
			model.NewUserMessage(userMsg))
		require.NoError(t, err)
		for range ch {
		}

		aReqs := modelA.getRequests()
		require.NotEmpty(t, aReqs)
		firstReq := aReqs[0]
		dumpRequestMessages(t, "agent_a entry (AgentOnly)", firstReq)

		require.True(t, requestContainsUserText(firstReq, userMsg),
			"AgentOnly entry: agent_a should see the originating user message via strict bypass")
	})

	t.Run("handoff_target_with_transfer_message", func(t *testing.T) {
		const transferMsg = "please analyze the Go ecosystem specifically"

		modelA := &isolationCaptureModel{
			name: "model-a-with-msg",
			respond: newTransferringAssistant(
				"Agent A thinks this is great! Let me transfer to agent B for analysis.",
				agentBName,
				transferMsg,
			),
		}
		modelB := &isolationCaptureModel{
			name:    "model-b-with-msg",
			respond: newStaticAssistant("Agent B analysis complete."),
		}

		agentA := llmagent.New(agentAName,
			llmagent.WithModel(modelA),
			llmagent.WithInstruction("You are agent A."),
		)
		agentB := llmagent.New(agentBName,
			llmagent.WithModel(modelB),
			llmagent.WithInstruction("You are agent B."),
		)

		cfg := DefaultSwarmConfig()
		cfg.HistoryScope = SwarmHistoryScopeAgentOnly
		tm, err := NewSwarm(tmName, agentAName,
			[]agent.Agent{agentA, agentB},
			WithSwarmConfig(cfg),
		)
		require.NoError(t, err)

		r := runner.NewRunner("test-app", tm,
			runner.WithSessionService(sessioninmemory.NewSessionService()),
		)
		defer r.Close()

		ch, err := r.Run(context.Background(), "u", "s-handoff-msg",
			model.NewUserMessage(userMsg))
		require.NoError(t, err)
		for range ch {
		}

		bReqs := modelB.getRequests()
		require.NotEmpty(t, bReqs)
		firstReq := bReqs[0]
		dumpRequestMessages(t, "agent_b handoff-with-msg (AgentOnly)", firstReq)

		require.True(t, requestContainsUserText(firstReq, transferMsg),
			"AgentOnly handoff w/ message: agent_b should see the transfer message")
		require.False(t, requestContainsAgentAContent(firstReq),
			"AgentOnly handoff w/ message: agent_b should not see agent_a's sibling output")
	})

	t.Run("handoff_target_without_transfer_message_inherits_source", func(t *testing.T) {
		// Empty transfer message: transfer.go line 174 will NOT overwrite
		// targetInvocation.Message, so the target inherits the source
		// invocation's Message, which at this point is the originating
		// user message.
		modelA := &isolationCaptureModel{
			name: "model-a-no-msg",
			respond: newTransferringAssistant(
				"Agent A transfers without a specific message.",
				agentBName,
				"", // empty on purpose
			),
		}
		modelB := &isolationCaptureModel{
			name:    "model-b-no-msg",
			respond: newStaticAssistant("Agent B analysis complete."),
		}

		agentA := llmagent.New(agentAName,
			llmagent.WithModel(modelA),
			llmagent.WithInstruction("You are agent A."),
		)
		agentB := llmagent.New(agentBName,
			llmagent.WithModel(modelB),
			llmagent.WithInstruction("You are agent B."),
		)

		cfg := DefaultSwarmConfig()
		cfg.HistoryScope = SwarmHistoryScopeAgentOnly
		tm, err := NewSwarm(tmName, agentAName,
			[]agent.Agent{agentA, agentB},
			WithSwarmConfig(cfg),
		)
		require.NoError(t, err)

		r := runner.NewRunner("test-app", tm,
			runner.WithSessionService(sessioninmemory.NewSessionService()),
		)
		defer r.Close()

		ch, err := r.Run(context.Background(), "u", "s-handoff-inherit",
			model.NewUserMessage(userMsg))
		require.NoError(t, err)
		for range ch {
		}

		bReqs := modelB.getRequests()
		require.NotEmpty(t, bReqs)
		firstReq := bReqs[0]
		dumpRequestMessages(t, "agent_b handoff-no-msg (AgentOnly)", firstReq)

		require.True(t, requestContainsUserText(firstReq, userMsg),
			"AgentOnly handoff w/o message: agent_b should see inherited source Message (the user message)")
		require.False(t, requestContainsAgentAContent(firstReq),
			"AgentOnly handoff w/o message: agent_b still should not see agent_a's sibling output")
	})
}

// TestSwarm_HistoryScope_DoesNotOverrideMemberIsolatedInvocation verifies
// the doc claim on SwarmHistoryScope that an agent-level explicit timeline
// filter (llmagent.WithMessageFilterMode(llmagent.IsolatedInvocation))
// takes precedence over the Swarm's HistoryScope: even under
// RootAndAgent (which would otherwise let the member see root context),
// a member that opts into IsolatedInvocation sees only its own invocation's
// messages — not just "the same number of messages", but the exact same
// set of messages as the IsolatedInvocation baseline.
func TestSwarm_HistoryScope_DoesNotOverrideMemberIsolatedInvocation(t *testing.T) {
	const (
		agentAName  = "agent_a"
		agentBName  = "agent_b"
		tmName      = "precedence-team"
		userMsg     = "What do you think about Go?"
		transferMsg = "please analyze"
		agentAText  = "Agent A thinks this is great! Let me transfer to agent B for analysis."
	)

	// Baseline: NO HistoryScope (i.e., Shared) + member has IsolatedInvocation.
	baselineReq := runPrecedenceCase(t,
		agentAName, agentBName, tmName+"-baseline",
		userMsg, transferMsg, agentAText,
		SwarmHistoryScopeShared,
		true,
	)

	// Precedence case: RootAndAgent would normally let agent_b see root
	// context, but because agent_b explicitly configures IsolatedInvocation,
	// the final visible messages should still match the baseline exactly.
	precedenceReq := runPrecedenceCase(t,
		agentAName, agentBName, tmName+"-precedence",
		userMsg, transferMsg, agentAText,
		SwarmHistoryScopeRootAndAgent,
		true,
	)

	baselineSig := messageSignatures(baselineReq)
	precedenceSig := messageSignatures(precedenceReq)

	// Hard content comparison: not just len(messages) but the full
	// role+content+tool_call+tool_id signature of every non-system message.
	// This catches regressions where the count stays the same but the
	// messages themselves differ (e.g., agent_a content leaks in under
	// RootAndAgent despite member-level IsolatedInvocation being set).
	require.Equal(t, baselineSig, precedenceSig,
		"member's IsolatedInvocation must take precedence over HistoryScope: "+
			"baseline (Shared+Isolated) and precedence (RootAndAgent+Isolated) "+
			"must surface the exact same non-system messages to agent_b")

	// Hard negative assertion: agent_a's sibling reply must NOT appear in
	// either case, regardless of HistoryScope. Defensive check in case the
	// structural equality above gets accidentally relaxed later.
	require.False(t, requestContainsAgentAContent(baselineReq),
		"baseline: agent_a's sibling text must not leak through IsolatedInvocation")
	require.False(t, requestContainsAgentAContent(precedenceReq),
		"precedence: agent_a's sibling text must not leak through IsolatedInvocation even under RootAndAgent")

	// Sanity: if member does NOT use IsolatedInvocation, RootAndAgent
	// surfaces strictly MORE context than the isolated baseline — proving
	// HistoryScope is doing real work when members don't opt out, and that
	// the precedence equality above is not a vacuous "everything is equal".
	unrestrictedReq := runPrecedenceCase(t,
		agentAName, agentBName, tmName+"-unrestricted",
		userMsg, transferMsg, agentAText,
		SwarmHistoryScopeRootAndAgent,
		false,
	)
	unrestrictedSig := messageSignatures(unrestrictedReq)
	require.Greater(t, len(unrestrictedSig), len(baselineSig),
		"sanity: without member-level IsolatedInvocation, RootAndAgent "+
			"should surface more messages than IsolatedInvocation alone "+
			"(baseline=%d, unrestricted=%d)", len(baselineSig), len(unrestrictedSig))
	// The unrestricted case must contain messages absent from the baseline
	// (otherwise our precedence test is degenerate).
	require.NotEqual(t, baselineSig, unrestrictedSig,
		"sanity: unrestricted case must differ from baseline in content, not just count")
}

// messageSignatures returns a deterministic, compact signature for every
// non-system message in req. It captures the bits that matter for
// user-visible context — role, content, tool-call identity, and tool
// result linkage — while ignoring presentation-only fields.
func messageSignatures(req *model.Request) []string {
	if req == nil {
		return nil
	}
	out := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem {
			continue
		}
		parts := []string{
			"role=" + string(msg.Role),
			"content=" + msg.Content,
		}
		for _, tc := range msg.ToolCalls {
			parts = append(parts,
				fmt.Sprintf("tool_call=%s(%s)",
					tc.Function.Name,
					string(tc.Function.Arguments)),
			)
		}
		if msg.ToolID != "" {
			parts = append(parts, "tool_id="+msg.ToolID)
		}
		out = append(out, strings.Join(parts, "|"))
	}
	return out
}

// runPrecedenceCase builds a two-member Swarm, runs it once, and returns
// the first request visible to agent_b. It factors out the setup
// boilerplate for TestSwarm_HistoryScope_DoesNotOverrideMemberIsolatedInvocation.
func runPrecedenceCase(
	t *testing.T,
	agentAName, agentBName, teamName,
	userMsg, transferMsg, agentAText string,
	scope SwarmHistoryScope,
	memberIsolated bool,
) *model.Request {
	t.Helper()

	modelA := &isolationCaptureModel{
		name:    "model-a-" + teamName,
		respond: newTransferringAssistant(agentAText, agentBName, transferMsg),
	}
	modelB := &isolationCaptureModel{
		name:    "model-b-" + teamName,
		respond: newStaticAssistant("Agent B analysis complete."),
	}

	genConfig := model.GenerationConfig{MaxTokens: intPtrIso(500), Stream: false}

	agentA := llmagent.New(agentAName,
		llmagent.WithModel(modelA),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithDescription("Agent A"),
		llmagent.WithInstruction("You are agent A."),
	)

	bOpts := []llmagent.Option{
		llmagent.WithModel(modelB),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithDescription("Agent B"),
		llmagent.WithInstruction("You are agent B."),
	}
	if memberIsolated {
		bOpts = append(bOpts,
			llmagent.WithMessageFilterMode(llmagent.IsolatedInvocation),
		)
	}
	agentB := llmagent.New(agentBName, bOpts...)

	cfg := DefaultSwarmConfig()
	cfg.HistoryScope = scope
	tm, err := NewSwarm(teamName, agentAName,
		[]agent.Agent{agentA, agentB},
		WithSwarmConfig(cfg),
	)
	require.NoError(t, err)

	r := runner.NewRunner("test-app", tm,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	defer r.Close()

	ch, err := r.Run(context.Background(), "u", "s-"+teamName,
		model.NewUserMessage(userMsg))
	require.NoError(t, err)
	for range ch {
	}

	bReqs := modelB.getRequests()
	require.NotEmpty(t, bReqs, "agent_b should have been called in case %q", teamName)
	firstReq := bReqs[0]
	dumpRequestMessages(t, teamName, firstReq)
	return firstReq
}

// pingPongTransferAssistant returns a respond function that issues a
// transfer_to_agent call on every call until maxTransfers is reached, and
// then emits a static reply without any tool calls. It is used to construct
// scenarios where the same agent is invoked more than once in a single
// Swarm run (e.g., A -> B -> A) so we can inspect what A sees on its second
// invocation.
func pingPongTransferAssistant(
	replyPrefix, targetAgent, transferMessage string,
	maxTransfers int,
) func(int, *model.Request) *model.Response {
	return func(idx int, _ *model.Request) *model.Response {
		if idx < maxTransfers {
			return &model.Response{
				Done: true,
				Choices: []model.Choice{{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: fmt.Sprintf("%s call=%d transferring", replyPrefix, idx),
						ToolCalls: []model.ToolCall{{
							ID: fmt.Sprintf("tc-transfer-%d", idx),
							Function: model.FunctionDefinitionParam{
								Name:      transfertool.TransferToolName,
								Arguments: buildTransferArgs(targetAgent, transferMessage),
							},
						}},
					},
				}},
			}
		}
		return &model.Response{
			Done: true,
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: fmt.Sprintf("%s call=%d final", replyPrefix, idx),
				},
			}},
		}
	}
}

// TestSwarm_HistoryScopeRootAndAgent_E2E_SameAgentMultipleHandoffsKeepsOwnHistory
// exercises the P1 claim on SwarmHistoryScopeRootAndAgent that a member keeps
// its own prior invocation history across handoffs: A -> B -> A, and on
// A's second invocation verify that A sees:
//   - the originating user message (root is shared via prefix),
//   - its own first reply (per-agent key matches exact prefix of itself),
//   - but NOT B's sibling reply.
func TestSwarm_HistoryScopeRootAndAgent_E2E_SameAgentMultipleHandoffsKeepsOwnHistory(t *testing.T) {
	const (
		agentAName = "agent_a"
		agentBName = "agent_b"
		tmName     = "root-agent-pingpong"
		userMsg    = "Original user question about Go"
		aReplyTag  = "AAAAA-unique-reply-from-agent-a"
		bReplyTag  = "BBBBB-unique-reply-from-agent-b"
	)

	modelA := &isolationCaptureModel{
		name:    "model-a",
		respond: pingPongTransferAssistant(aReplyTag, agentBName, "please analyze", 1),
	}
	modelB := &isolationCaptureModel{
		name:    "model-b",
		respond: pingPongTransferAssistant(bReplyTag, agentAName, "come back", 1),
	}

	agentA := llmagent.New(agentAName,
		llmagent.WithModel(modelA),
		llmagent.WithInstruction("You are agent A."),
	)
	agentB := llmagent.New(agentBName,
		llmagent.WithModel(modelB),
		llmagent.WithInstruction("You are agent B."),
	)

	cfg := DefaultSwarmConfig()
	cfg.HistoryScope = SwarmHistoryScopeRootAndAgent
	tm, err := NewSwarm(tmName, agentAName,
		[]agent.Agent{agentA, agentB},
		WithSwarmConfig(cfg),
	)
	require.NoError(t, err)

	r := runner.NewRunner("test-app", tm,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	defer r.Close()

	ch, err := r.Run(context.Background(), "u", "s-root-pingpong",
		model.NewUserMessage(userMsg))
	require.NoError(t, err)
	for evt := range ch {
		if evt != nil && evt.Error != nil {
			t.Logf("evt error from %s: %s", evt.Author, evt.Error.Message)
		}
	}

	aReqs := modelA.getRequests()
	require.Len(t, aReqs, 2, "agent_a should be invoked twice (entry + handoff back)")
	secondReq := aReqs[1]
	dumpRequestMessages(t, "agent_a second call (RootAndAgent)", secondReq)

	require.True(t, requestContainsUserText(secondReq, userMsg),
		"RootAndAgent: on second call agent_a should still see the original user msg via root prefix")
	require.True(t, requestContainsUserText(secondReq, aReplyTag),
		"RootAndAgent: on second call agent_a should see its own prior reply (per-agent key)")
	require.False(t, requestContainsUserText(secondReq, bReplyTag),
		"RootAndAgent: on second call agent_a must NOT see sibling agent_b's reply")
}

// TestSwarm_HistoryScopeAgentOnly_E2E_SameAgentMultipleHandoffsKeepsOwnHistory
// exercises the same P1 claim under SwarmHistoryScopeAgentOnly. Here A's
// per-agent key is intentionally disjoint from the root, so on A's second
// invocation verify that A sees:
//   - its own first reply (stable per-agent key matches across invocations),
//   - but NOT B's reply,
//   - AND NOT the originating user message under the root (AgentOnly does
//     not inherit the shared root prefix).
//
// The user message may still appear via the strict-invocation bypass if the
// second invocation's input happens to equal it; to avoid conflating the
// two paths we have B transfer back with an explicit, distinct transfer
// message ("come back"), which becomes A's second-invocation input and is
// not the original user question.
func TestSwarm_HistoryScopeAgentOnly_E2E_SameAgentMultipleHandoffsKeepsOwnHistory(t *testing.T) {
	const (
		agentAName = "agent_a"
		agentBName = "agent_b"
		tmName     = "agent-only-pingpong"
		userMsg    = "Original user question about Go"
		aReplyTag  = "AAAAA-unique-reply-from-agent-a"
		bReplyTag  = "BBBBB-unique-reply-from-agent-b"
	)

	modelA := &isolationCaptureModel{
		name:    "model-a",
		respond: pingPongTransferAssistant(aReplyTag, agentBName, "please analyze", 1),
	}
	modelB := &isolationCaptureModel{
		name:    "model-b",
		respond: pingPongTransferAssistant(bReplyTag, agentAName, "come back", 1),
	}

	agentA := llmagent.New(agentAName,
		llmagent.WithModel(modelA),
		llmagent.WithInstruction("You are agent A."),
	)
	agentB := llmagent.New(agentBName,
		llmagent.WithModel(modelB),
		llmagent.WithInstruction("You are agent B."),
	)

	cfg := DefaultSwarmConfig()
	cfg.HistoryScope = SwarmHistoryScopeAgentOnly
	tm, err := NewSwarm(tmName, agentAName,
		[]agent.Agent{agentA, agentB},
		WithSwarmConfig(cfg),
	)
	require.NoError(t, err)

	r := runner.NewRunner("test-app", tm,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	defer r.Close()

	ch, err := r.Run(context.Background(), "u", "s-agent-only-pingpong",
		model.NewUserMessage(userMsg))
	require.NoError(t, err)
	for evt := range ch {
		if evt != nil && evt.Error != nil {
			t.Logf("evt error from %s: %s", evt.Author, evt.Error.Message)
		}
	}

	aReqs := modelA.getRequests()
	require.Len(t, aReqs, 2, "agent_a should be invoked twice")
	secondReq := aReqs[1]
	dumpRequestMessages(t, "agent_a second call (AgentOnly)", secondReq)

	require.True(t, requestContainsUserText(secondReq, aReplyTag),
		"AgentOnly: on second call agent_a should still see its own prior reply via stable per-agent key")
	require.False(t, requestContainsUserText(secondReq, bReplyTag),
		"AgentOnly: on second call agent_a must NOT see sibling agent_b's reply")
	require.False(t, requestContainsUserText(secondReq, userMsg),
		"AgentOnly: on second call agent_a should NOT see the original user message (detached from root)")
}

// TestSwarm_Transfer_E2E_FilterKeyDerivationForThreeScopes verifies what
// filter key each member's invocation actually carries at model-call time
// under each SwarmHistoryScope. This closes the loop on the Transfer path:
// PrepareInvocationForAgent must run on the transfer target just like it
// does on the entry member, so the target's derived key matches the
// documented contract for its scope. Without this hook, a transfer target
// would inherit the source's key (the old unfiltered Swarm behavior).
//
// Expected keys under runner app name "test-app":
//   - Shared:        both members see the session root "test-app".
//   - RootAndAgent:  "test-app/__swarm__/{team}/{member}" per member.
//   - AgentOnly:     "__swarm_agent__::{team}::{member}" per member.
func TestSwarm_Transfer_E2E_FilterKeyDerivationForThreeScopes(t *testing.T) {
	const (
		agentAName = "agent_a"
		agentBName = "agent_b"
		tmName     = "key-derive-team"
		userMsg    = "derive keys please"
		appName    = "test-app"
	)

	cases := []struct {
		name     string
		scope    SwarmHistoryScope
		wantKeyA string
		wantKeyB string
	}{
		{
			name:     "Shared",
			scope:    SwarmHistoryScopeShared,
			wantKeyA: appName,
			wantKeyB: appName,
		},
		{
			name:  "RootAndAgent",
			scope: SwarmHistoryScopeRootAndAgent,
			wantKeyA: joinFilterKey(appName,
				swarmHistoryFilterMarker, tmName, agentAName),
			wantKeyB: joinFilterKey(appName,
				swarmHistoryFilterMarker, tmName, agentBName),
		},
		{
			name:     "AgentOnly",
			scope:    SwarmHistoryScopeAgentOnly,
			wantKeyA: swarmAgentOnlyFilterPrefix + "::" + tmName + "::" + agentAName,
			wantKeyB: swarmAgentOnlyFilterPrefix + "::" + tmName + "::" + agentBName,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			modelA := &isolationCaptureModel{
				name:    "model-a-" + tc.name,
				respond: newTransferringAssistant("A replies", agentBName, "go"),
			}
			modelB := &isolationCaptureModel{
				name:    "model-b-" + tc.name,
				respond: newStaticAssistant("B replies"),
			}

			agentA := llmagent.New(agentAName,
				llmagent.WithModel(modelA),
				llmagent.WithInstruction("You are agent A."),
			)
			agentB := llmagent.New(agentBName,
				llmagent.WithModel(modelB),
				llmagent.WithInstruction("You are agent B."),
			)

			cfg := DefaultSwarmConfig()
			cfg.HistoryScope = tc.scope
			tm, err := NewSwarm(tmName, agentAName,
				[]agent.Agent{agentA, agentB},
				WithSwarmConfig(cfg),
			)
			require.NoError(t, err)

			r := runner.NewRunner(appName, tm,
				runner.WithSessionService(sessioninmemory.NewSessionService()),
			)
			defer r.Close()

			ch, err := r.Run(context.Background(), "u",
				"s-key-derive-"+tc.name,
				model.NewUserMessage(userMsg))
			require.NoError(t, err)
			for range ch {
			}

			aKeys := modelA.getFilterKeys()
			bKeys := modelB.getFilterKeys()
			require.NotEmpty(t, aKeys, "agent_a should have been invoked")
			require.NotEmpty(t, bKeys, "agent_b should have been invoked")
			t.Logf("scope=%s agent_a keys=%v agent_b keys=%v",
				tc.name, aKeys, bKeys)

			require.Equal(t, tc.wantKeyA, aKeys[0],
				"agent_a (entry) filter key mismatch under %s", tc.name)
			require.Equal(t, tc.wantKeyB, bKeys[0],
				"agent_b (transfer target) filter key mismatch under %s", tc.name)
		})
	}
}
