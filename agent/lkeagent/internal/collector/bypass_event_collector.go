//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package collector provides internal event collection functionality for LKE Agent.
package collector

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	lkesdk "github.com/tencent-lke/lke-sdk-go"
	lkeevent "github.com/tencent-lke/lke-sdk-go/event"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// BypassEventCollector implements lkesdk.EventHandler interface
// and bridges LKE callback events to trpc-agent-go event streams.
// This allows the original LKE callback logic to continue working
// while simultaneously providing events to the trpc-agent-go ecosystem.
type BypassEventCollector struct {
	// original wraps the user's existing EventHandler
	original lkesdk.EventHandler
	// eventChan is the bypass channel to send events to trpc-agent-go
	eventChan chan *event.Event
	// processing indicates if we're currently processing a request
	processing bool
	// invocationID tracks the current invocation
	invocationID string
	// enableBypass controls whether to send events to trpc-agent-go
	enableBypass bool
	// agentName is the name of the agent for event identification
	agentName string
	// debug enables debug logging for event handling
	debug bool
	mu    sync.RWMutex
}

// New creates a new bypass event collector.
func New(original lkesdk.EventHandler, enableBypass bool, agentName string, debug bool) *BypassEventCollector {
	return &BypassEventCollector{
		original:     original,
		enableBypass: enableBypass,
		agentName:    agentName,
		debug:        debug,
	}
}

// StartProcessing enables event collection to the bypass channel.
func (b *BypassEventCollector) StartProcessing(eventChan chan *event.Event, invocationID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.eventChan = eventChan
	b.invocationID = invocationID
	b.processing = true
}

// StopProcessing disables event collection.
func (b *BypassEventCollector) StopProcessing() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.processing = false
	b.eventChan = nil
	b.invocationID = ""
}

// Cleanup performs resource cleanup for the event collector.
// This should be called when the collector is no longer needed.
func (b *BypassEventCollector) Cleanup() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Stop processing if still active
	b.processing = false
	b.eventChan = nil
	b.invocationID = ""

	// Clear original handler reference to help garbage collection
	b.original = nil

	if b.debug {
		log.Printf("[LKEAgent:%s] Event collector cleanup completed", b.agentName)
	}
}

// sendEvent safely sends an event to the bypass channel if processing is active and bypass is enabled.
func (b *BypassEventCollector) sendEvent(evt *event.Event) {
	b.mu.RLock()
	// Create local copies of the fields we need to check to avoid holding the lock longer than necessary
	enableBypass := b.enableBypass
	processing := b.processing
	eventChan := b.eventChan
	debug := b.debug
	agentName := b.agentName
	b.mu.RUnlock()

	// Only send to bypass channel if bypass is enabled and processing is active
	if enableBypass && processing && eventChan != nil {
		select {
		case eventChan <- evt:
			// Event sent successfully
			if debug {
				log.Printf("[LKEAgent:%s] Event sent: %s", agentName, evt.Response.Object)
			}
		default:
			// Channel is full, drop event to prevent blocking
			if debug {
				log.Printf("[LKEAgent:%s] WARNING: Event dropped due to full channel: %s", agentName, evt.Response.Object)
			}
		}
	} else if debug && enableBypass {
		// Debug log for why event was not sent
		if !processing {
			log.Printf("[LKEAgent:%s] Event not sent: processing not active", agentName)
		} else if eventChan == nil {
			log.Printf("[LKEAgent:%s] Event not sent: eventChan is nil", agentName)
		}
	}
}

// OnError implements lkesdk.EventHandler interface.
func (b *BypassEventCollector) OnError(err *lkeevent.ErrorEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnError(err)
	}

	// Get invocation ID safely
	b.mu.RLock()
	invocationID := b.invocationID
	b.mu.RUnlock()

	// Send to bypass channel
	errorEvent := event.NewErrorEvent(invocationID, b.agentName,
		model.ErrorTypeAPIError,
		fmt.Sprintf("LKE error: %s (code: %d)", err.Error.Message, err.Error.Code))
	b.sendEvent(errorEvent)
}

// OnReply implements lkesdk.EventHandler interface.
func (b *BypassEventCollector) OnReply(reply *lkeevent.ReplyEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnReply(reply)
	}

	// Send to bypass channel
	if !reply.IsFromSelf && reply.IsFinal {
		// Get invocation ID safely
		b.mu.RLock()
		invocationID := b.invocationID
		b.mu.RUnlock()

		response := &model.Response{
			Object:  model.ObjectTypeChatCompletion,
			Created: time.Now().Unix(),
			Done:    true,
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Content: reply.Content,
					Role:    model.RoleAssistant,
				},
			}},
		}
		replyEvent := event.NewResponseEvent(invocationID, b.agentName, response)
		b.sendEvent(replyEvent)
	}
}

// OnThought implements lkesdk.EventHandler interface.
func (b *BypassEventCollector) OnThought(thought *lkeevent.AgentThoughtEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnThought(thought)
	}

	// Send to bypass channel
	if len(thought.Procedures) > 0 {
		// Get invocation ID safely
		b.mu.RLock()
		invocationID := b.invocationID
		b.mu.RUnlock()

		procedure := thought.Procedures[len(thought.Procedures)-1]
		response := &model.Response{
			Object:  model.ObjectTypePreprocessingBasic,
			Created: time.Now().Unix(),
			Choices: []model.Choice{{
				Index: 0,
				Message: model.Message{
					Content: procedure.Debugging.Content,
					Role:    model.RoleAssistant,
				},
			}},
		}
		thoughtEvent := event.NewResponseEvent(invocationID, b.agentName, response)
		b.sendEvent(thoughtEvent)
	}
}

// OnReference implements lkesdk.EventHandler interface.
func (b *BypassEventCollector) OnReference(refer *lkeevent.ReferenceEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnReference(refer)
	}

	// Send to bypass channel if needed
}

// OnTokenStat implements lkesdk.EventHandler interface.
func (b *BypassEventCollector) OnTokenStat(stat *lkeevent.TokenStatEvent) {
	// Call original handler first
	if b.original != nil {
		b.original.OnTokenStat(stat)
	}

	// Send to bypass channel if needed
}

// BeforeToolCallHook implements lkesdk.EventHandler interface.
func (b *BypassEventCollector) BeforeToolCallHook(toolCallCtx lkesdk.ToolCallContext) {
	// Call original handler first
	if b.original != nil {
		b.original.BeforeToolCallHook(toolCallCtx)
	}

	// Get invocation ID safely
	b.mu.RLock()
	invocationID := b.invocationID
	b.mu.RUnlock()

	// Send to bypass channel - tool calls use a different structure in trpc-agent-go
	argsJSON, err := json.Marshal(toolCallCtx.Input)
	if err != nil {
		// If serialization fails, use empty JSON object
		argsJSON = []byte("{}")
	}
	response := &model.Response{
		Object:  model.ObjectTypeToolResponse,
		Created: time.Now().Unix(),
		Choices: []model.Choice{{
			Index: 0,
			Message: model.Message{
				Content: fmt.Sprintf("Tool call started: %s with args: %s",
					toolCallCtx.CallTool.GetName(), string(argsJSON)),
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					ID:   toolCallCtx.CallId,
					Type: "function",
					Function: model.FunctionDefinitionParam{
						Name: toolCallCtx.CallTool.GetName(),
					},
				}},
			},
		}},
	}
	toolCallEvent := event.NewResponseEvent(invocationID, b.agentName, response)
	b.sendEvent(toolCallEvent)
}

// AfterToolCallHook implements lkesdk.EventHandler interface.
func (b *BypassEventCollector) AfterToolCallHook(toolCallCtx lkesdk.ToolCallContext) {
	// Call original handler first
	if b.original != nil {
		b.original.AfterToolCallHook(toolCallCtx)
	}

	// Get invocation ID safely
	b.mu.RLock()
	invocationID := b.invocationID
	b.mu.RUnlock()

	// Send to bypass channel
	var content string
	if toolCallCtx.Err != nil {
		content = fmt.Sprintf("Tool call failed: %s (error: %v)",
			toolCallCtx.CallTool.GetName(), toolCallCtx.Err)
	} else {
		content = fmt.Sprintf("Tool call succeeded: %s (result: %s)",
			toolCallCtx.CallTool.GetName(), toolCallCtx.CallTool.ResultToString(toolCallCtx.Output))
	}

	response := &model.Response{
		Object:  model.ObjectTypeToolResponse,
		Created: time.Now().Unix(),
		Choices: []model.Choice{{
			Index: 0,
			Message: model.Message{
				Content:  content,
				Role:     model.RoleTool,
				ToolID:   toolCallCtx.CallId,
				ToolName: toolCallCtx.CallTool.GetName(),
			},
		}},
	}
	toolResultEvent := event.NewResponseEvent(invocationID, b.agentName, response)
	b.sendEvent(toolResultEvent)
}
