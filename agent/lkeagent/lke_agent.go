//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package lkeagent provides adapters to integrate Tencent Cloud LKE SDK with trpc-agent-go.
// This package enables seamless migration from LKE-based agents to trpc-agent-go ecosystem.
package lkeagent

import (
	"context"
	"fmt"
	"time"

	lkesdk "github.com/tencent-lke/lke-sdk-go"
	lkemodel "github.com/tencent-lke/lke-sdk-go/model"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/lkeagent/internal/collector"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// LKEAgent wraps a Tencent Cloud LKE SDK client as a trpc-agent-go Agent.
// This adapter allows LKE-based agents to integrate seamlessly into the trpc-agent-go ecosystem
// while preserving their original callback-based event handling.
type LKEAgent struct {
	name      string
	lkeClient lkesdk.LkeClient
	info      agent.Info
	options   *config

	// eventCollector implements LKE EventHandler interface and bridges
	// callback events to trpc-agent-go event streams
	eventCollector *collector.BypassEventCollector
}

// config holds configuration options for LKE Agent.
type config struct {
	// name of the agent
	name string
	// originalHandler preserves existing business logic
	originalHandler lkesdk.EventHandler
	// debug enables debug logging
	debug bool
	// bufferSize sets the event channel buffer size
	bufferSize int
	// visitorBizID for LKE client
	visitorBizID string
	// lkeOptions for LKE client
	lkeOptions *lkemodel.Options
	// enableEventBypass controls whether to send events to trpc-agent-go event stream
	// When true (default): LKE events are converted to trpc-agent-go events
	// When false: Only original handler logic works, no event stream
	enableEventBypass bool
}

// Option defines a configuration option for LKE Agent
type Option func(*config)

// WithName sets the agent name
func WithName(name string) Option {
	return func(c *config) {
		c.name = name
	}
}

// WithOriginalHandler sets the original EventHandler to preserve existing business logic
func WithOriginalHandler(handler lkesdk.EventHandler) Option {
	return func(c *config) {
		c.originalHandler = handler
	}
}

// WithVisitorBizID sets the visitor business ID for LKE client
func WithVisitorBizID(visitorBizID string) Option {
	return func(c *config) {
		c.visitorBizID = visitorBizID
	}
}

// WithBufferSize sets the event channel buffer size
func WithBufferSize(size int) Option {
	return func(c *config) {
		c.bufferSize = size
	}
}

// WithEventBypass controls whether to send events to trpc-agent-go event stream
func WithEventBypass(enable bool) Option {
	return func(c *config) {
		c.enableEventBypass = enable
	}
}

// WithLKEOptions sets LKE client options
func WithLKEOptions(opts *lkemodel.Options) Option {
	return func(c *config) {
		c.lkeOptions = opts
	}
}

// WithDebug enables debug logging
func WithDebug(debug bool) Option {
	return func(c *config) {
		c.debug = debug
	}
}

// New creates a new LKE Agent adapter with functional options.
// The lkeClient parameter is required and cannot be nil.
// Optional configurations can be provided through WithOption functions.
func New(lkeClient lkesdk.LkeClient, opts ...Option) agent.Agent {
	// Validate required parameters
	if lkeClient == nil {
		panic("lkeClient cannot be nil")
	}

	// Default configuration
	config := &config{
		name:              "lke-agent", // Default name
		bufferSize:        50,          // Default buffer size
		enableEventBypass: true,        // Default to true for event support
	}

	// Apply all options
	for _, opt := range opts {
		opt(config)
	}

	// Validate configuration
	if config.bufferSize <= 0 {
		config.bufferSize = 50 // Fallback to default
	}
	if config.name == "" {
		config.name = "lke-agent" // Fallback to default
	}

	// Smart defaults: if no original handler and bypass disabled, enable bypass anyway
	// because otherwise there would be no way to get events
	if !config.enableEventBypass && config.originalHandler == nil {
		config.enableEventBypass = true
	}

	eventCollector := collector.New(config.originalHandler, config.enableEventBypass, config.name, config.debug)

	// Replace the LKE client's event handler with our bypass collector
	lkeClient.SetEventHandler(eventCollector)

	return &LKEAgent{
		name:           config.name,
		lkeClient:      lkeClient,
		eventCollector: eventCollector,
		options:        config,
		info: agent.Info{
			Name:        config.name,
			Description: "LKE Agent integrated into trpc-agent-go ecosystem",
		},
	}
}

// Run implements agent.Agent interface.
func (a *LKEAgent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	eventChan := make(chan *event.Event, a.options.bufferSize)

	go func() {
		defer close(eventChan)

		// Only start event collection if bypass is enabled
		if a.options.enableEventBypass {
			// Start event collection
			a.eventCollector.StartProcessing(eventChan, invocation.InvocationID)
			defer a.eventCollector.StopProcessing()
		}

		// Extract query from invocation
		query := invocation.Message.Content
		if query == "" {
			// Empty query indicates caller error, should not provide meaningless default
			if a.options.enableEventBypass {
				errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name,
					model.ErrorTypeAPIError,
					"Empty query provided: invocation.Message.Content is required")
				eventChan <- errorEvent
			}
			return
		}

		// Generate session ID from invocation context
		sessionID := invocation.InvocationID
		if sessionID == "" {
			// InvocationID should be provided by framework, but provide fallback for robustness
			sessionID = fmt.Sprintf("auto-session-%d", time.Now().UnixMilli())
		}

		// Use configured visitor ID
		visitorBizID := a.options.visitorBizID
		if visitorBizID == "" {
			// If not configured, use session ID as visitor ID
			visitorBizID = fmt.Sprintf("visitor-%s", sessionID)
		}

		// Call LKE client (this will trigger callbacks which feed our bypass)
		finalReply, err := a.lkeClient.RunWithContext(ctx, query, sessionID, visitorBizID, a.options.lkeOptions)

		// Send final result or error only if bypass is enabled
		if a.options.enableEventBypass {
			if err != nil {
				errorEvent := event.NewErrorEvent(invocation.InvocationID, a.name,
					model.ErrorTypeAPIError,
					fmt.Sprintf("LKE execution failed: %v", err))
				eventChan <- errorEvent
			} else if finalReply != nil && !finalReply.IsFromSelf {
				response := &model.Response{
					Object:  model.ObjectTypeChatCompletion,
					Created: time.Now().Unix(),
					Done:    true,
					Choices: []model.Choice{{
						Index: 0,
						Message: model.Message{
							Content: finalReply.Content,
							Role:    model.RoleAssistant,
						},
					}},
				}
				finalEvent := event.NewResponseEvent(invocation.InvocationID, a.name, response)
				eventChan <- finalEvent
			}
		}
	}()

	return eventChan, nil
}

// Tools implements agent.Agent interface.
// LKE agents manage their tools internally, so we return an empty slice.
// The actual tools are configured within the LKE client.
func (a *LKEAgent) Tools() []tool.Tool {
	// LKE SDK manages tools internally through its client configuration
	// We don't expose them here to avoid conflicts with trpc-agent-go tool management
	return []tool.Tool{}
}

// Info implements agent.Agent interface.
func (a *LKEAgent) Info() agent.Info {
	return a.info
}

// SubAgents implements agent.Agent interface.
func (a *LKEAgent) SubAgents() []agent.Agent {
	// LKE agents don't have sub-agents in the trpc-agent-go sense
	return []agent.Agent{}
}

// FindSubAgent implements agent.Agent interface.
func (a *LKEAgent) FindSubAgent(name string) agent.Agent {
	// LKE agents don't have sub-agents in the trpc-agent-go sense
	return nil
}

// Close closes the LKE client and releases resources.
func (a *LKEAgent) Close() error {
	// Clean up event collector first
	if a.eventCollector != nil {
		a.eventCollector.Cleanup()
	}

	// Close LKE client
	if a.lkeClient != nil {
		a.lkeClient.Close()
	}

	// Clear references to help garbage collection
	a.eventCollector = nil
	a.lkeClient = nil
	a.options = nil

	return nil
}

// setVisitorBizID updates the visitor business ID for LKE client calls.
func (a *LKEAgent) setVisitorBizID(visitorBizID string) {
	if a.options != nil {
		a.options.visitorBizID = visitorBizID
	}
}

// setLKEOptions updates the LKE client options.
func (a *LKEAgent) setLKEOptions(options *lkemodel.Options) {
	if a.options != nil {
		a.options.lkeOptions = options
	}
}
