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
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	transfertool "trpc.group/trpc-go/trpc-agent-go/tool/transfer"
)

type isolationCaptureModel struct {
	mu         sync.Mutex
	requests   []*model.Request
	filterKeys []string
	name       string
	respond    func(callIdx int, req *model.Request) *model.Response
	callIdx    int
}

func (m *isolationCaptureModel) GenerateContent(
	ctx context.Context,
	req *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.callIdx
	m.callIdx++
	reqCopy := &model.Request{Messages: make([]model.Message, len(req.Messages))}
	copy(reqCopy.Messages, req.Messages)
	m.requests = append(m.requests, reqCopy)
	var filterKey string
	if inv, ok := agent.InvocationFromContext(ctx); ok && inv != nil {
		filterKey = inv.GetEventFilterKey()
	}
	m.filterKeys = append(m.filterKeys, filterKey)
	resp := m.respond(idx, req)
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *isolationCaptureModel) Info() model.Info {
	return model.Info{Name: m.name}
}

func (m *isolationCaptureModel) getRequests() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*model.Request, len(m.requests))
	copy(out, m.requests)
	return out
}

func (m *isolationCaptureModel) getFilterKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.filterKeys))
	copy(out, m.filterKeys)
	return out
}

func buildTransferArgs(targetAgent, message string) json.RawMessage {
	req := transfertool.Request{AgentName: targetAgent, Message: message}
	b, _ := json.Marshal(req)
	return b
}

func dumpRequestMessages(t *testing.T, label string, req *model.Request) {
	t.Helper()
	t.Logf("=== %s: %d messages ===", label, len(req.Messages))
	for i, msg := range req.Messages {
		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		t.Logf("  [%d] role=%-10s content=%q", i, msg.Role, content)
		for _, tc := range msg.ToolCalls {
			t.Logf("       tool_call: %s(%s)", tc.Function.Name, string(tc.Function.Arguments))
		}
		if msg.ToolID != "" {
			t.Logf("       tool_id: %s", msg.ToolID)
		}
	}
}

func requestContainsAgentAContent(req *model.Request) bool {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem {
			continue
		}
		if strings.Contains(msg.Content, "Agent A thinks this is great") {
			return true
		}
		if strings.Contains(msg.Content, "agent_a") &&
			strings.Contains(msg.Content, "said") {
			return true
		}
	}
	return false
}

func intPtrIso(v int) *int { return &v }

func TestSwarmIsolation_DefaultVsIsolated(t *testing.T) {
	const (
		agentAName = "agent_a"
		agentBName = "agent_b"
		tmName     = "iso-team"
		userMsg    = "What do you think about Go?"
	)

	tests := []struct {
		name     string
		isolated bool
	}{
		{
			name:     "default_no_isolation",
			isolated: false,
		},
		{
			name:     "IsolatedInvocation",
			isolated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modelA := &isolationCaptureModel{
				name: "model-a",
				respond: func(_ int, _ *model.Request) *model.Response {
					return &model.Response{
						ID:    "resp-a",
						Model: "model-a",
						Done:  true,
						Choices: []model.Choice{{
							Message: model.Message{
								Role:    model.RoleAssistant,
								Content: "Agent A thinks this is great! Let me transfer to agent B for analysis.",
								ToolCalls: []model.ToolCall{{
									ID: "tc-1",
									Function: model.FunctionDefinitionParam{
										Name:      transfertool.TransferToolName,
										Arguments: buildTransferArgs(agentBName, userMsg),
									},
								}},
							},
						}},
					}
				},
			}

			modelB := &isolationCaptureModel{
				name: "model-b",
				respond: func(_ int, _ *model.Request) *model.Response {
					return &model.Response{
						ID:    "resp-b",
						Model: "model-b",
						Done:  true,
						Choices: []model.Choice{{
							Message: model.Message{
								Role:    model.RoleAssistant,
								Content: "Agent B analysis complete.",
							},
						}},
					}
				},
			}

			genConfig := model.GenerationConfig{
				MaxTokens: intPtrIso(500),
				Stream:    false,
			}

			agentA := llmagent.New(
				agentAName,
				llmagent.WithModel(modelA),
				llmagent.WithGenerationConfig(genConfig),
				llmagent.WithDescription("Agent A: the optimist"),
				llmagent.WithInstruction("You are agent A."),
			)

			agentBOpts := []llmagent.Option{
				llmagent.WithModel(modelB),
				llmagent.WithGenerationConfig(genConfig),
				llmagent.WithDescription("Agent B: the analyst"),
				llmagent.WithInstruction("You are agent B."),
			}
			if tt.isolated {
				agentBOpts = append(agentBOpts,
					llmagent.WithMessageFilterMode(llmagent.IsolatedInvocation),
				)
			}
			agentB := llmagent.New(agentBName, agentBOpts...)

			tm, err := NewSwarm(tmName, agentAName, []agent.Agent{agentA, agentB})
			require.NoError(t, err)

			sessionService := sessioninmemory.NewSessionService()
			r := runner.NewRunner("test-app", tm, runner.WithSessionService(sessionService))
			defer r.Close()

			ctx := context.Background()
			eventChan, err := r.Run(ctx, "test-user", "test-session-"+tt.name,
				model.NewUserMessage(userMsg))
			require.NoError(t, err)

			for evt := range eventChan {
				if evt != nil && evt.Error != nil {
					t.Logf("Event error from %s: %s", evt.Author, evt.Error.Message)
				}
			}

			bRequests := modelB.getRequests()
			require.NotEmpty(t, bRequests, "agent_b model should have been called")

			firstReq := bRequests[0]
			dumpRequestMessages(t,
				fmt.Sprintf("agent_b (isolated=%v)", tt.isolated), firstReq)

			seesAgentA := requestContainsAgentAContent(firstReq)

			if tt.isolated {
				require.False(t, seesAgentA,
					"With IsolatedInvocation, agent_b should NOT see agent_a's content")
				t.Log("PASS: agent_b context is isolated from agent_a")
			} else {
				require.True(t, seesAgentA,
					"Without isolation, agent_b SHOULD see agent_a's content")
				t.Log("PASS: agent_b sees agent_a's context (expected)")
			}
		})
	}
}
