package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	lkesdk "github.com/tencent-lke/lke-sdk-go"
	lkeevent "github.com/tencent-lke/lke-sdk-go/event"
	lkemodel "github.com/tencent-lke/lke-sdk-go/model"
	lketool "github.com/tencent-lke/lke-sdk-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	lkeagent "trpc.group/trpc-go/trpc-agent-go/agent/lkeagent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	a2a "trpc.group/trpc-go/trpc-agent-go/server/a2a"
)

// ========== 🎯 Core demo: main function ==========
func main() {
	log.Println("🚀 LKE Agent → trpc-agent-go Adapter Demo")

	// ===== Core transformation: LKE Agent → trpc-agent-go Agent =====
	sessionID := "download-session-123"
	userID := "user-456"
	existingDownloadAgent := createExistingLKEAgent(sessionID, userID)

	// 🎯 Core functionality - wrap with one line of code using WithOption pattern
	downloadAgent := lkeagent.New(existingDownloadAgent.lkeClient,
		lkeagent.WithName("download-agent"),
		lkeagent.WithOriginalHandler(existingDownloadAgent.eventHandler),
		lkeagent.WithVisitorBizID(userID),
		lkeagent.WithBufferSize(50),
		lkeagent.WithLKEOptions(&lkemodel.Options{
			StreamingThrottle: 20,
			CustomVariables: map[string]string{
				"_user_guid":    userID,
				"_user_task_id": sessionID,
			},
		}),
	)
	log.Println("✅ Successfully wrapped as trpc-agent-go Agent")

	// ===== Demonstrate 4 usage modes =====
	ctx := context.Background()

	demonstrateRunnerMode(ctx, downloadAgent, userID)
	demonstrateSubAgentMode(downloadAgent)
	demonstrateA2AMode(downloadAgent)
	demonstrateMultiAgentMode(downloadAgent)

	// ===== Verify existing business logic still works =====
	demonstrateExistingAPI(sessionID)

	// ===== Demonstrate how to extract LKE callback data from events =====
	demonstrateEventDataExtraction(downloadAgent, sessionID)

	// ===== Test without originalHandler =====
	demonstrateNoOriginalHandler()

	// ===== Test disable event bypass =====
	demonstrateDisableEventBypass()

	log.Println("\n ✅ Demo completed!")
}

// ========== 🔧 Demo functions ==========

// demonstrateEventDataExtraction demonstrates how to extract LKE callback data from trpc-agent-go events
func demonstrateEventDataExtraction(downloadAgent agent.Agent, sessionID string) {
	log.Println("\n🔍 Demo: Extract LKE callback data from trpc-agent-go event stream")

	ctx := context.Background()
	invocation := &agent.Invocation{
		InvocationID: sessionID + "-event-demo",
		Message: model.Message{
			Content: "Demo event data extraction: download https://example.com/demo.pdf to /downloads/",
			Role:    model.RoleUser,
		},
	}

	eventChan, err := downloadAgent.Run(ctx, invocation)
	if err != nil {
		log.Printf("   ❌ Agent execution failed: %v", err)
		return
	}

	log.Println("   📡 Listening to event stream, extracting LKE callback data...")

	// Statistics for different types of events
	eventStats := map[string]int{
		"thought":     0,
		"tool_call":   0,
		"tool_result": 0,
		"final_reply": 0,
		"error":       0,
	}

	// Process event stream
	for evt := range eventChan {
		if evt.Response != nil {
			// Check for errors
			if evt.Response.Error != nil {
				eventStats["error"]++
				log.Printf("   ❌ [Event Data] Error: %s", evt.Response.Error.Message)
				continue
			}

			// Process normal responses
			if len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[0]

				switch evt.Response.Object {
				case model.ObjectTypeChatCompletion:
					eventStats["final_reply"]++
					log.Printf("   💬 [Event Data] Final reply: %s", choice.Message.Content)

				case "preprocessing.basic": // Thought events
					eventStats["thought"]++
					log.Printf("   🧠 [Event Data] Thought process: %s", choice.Message.Content)

				case model.ObjectTypeToolResponse:
					if len(choice.Message.ToolCalls) > 0 {
						eventStats["tool_call"]++
						toolCall := choice.Message.ToolCalls[0]
						log.Printf("   🛠️ [Event Data] Tool call: %s (ID: %s)",
							toolCall.Function.Name, toolCall.ID)

						// Show complete tool call data structure
						log.Printf("      ↳ Tool type: %s", toolCall.Type)
						if len(toolCall.Function.Arguments) > 0 {
							log.Printf("      ↳ Call arguments: %s", string(toolCall.Function.Arguments))
						}
					} else if choice.Message.Role == model.RoleTool {
						eventStats["tool_result"]++
						log.Printf("   📄 [Event Data] Tool result: %s", choice.Message.Content)
					}
				}
			}
		}

		// Check if completed
		if evt.Response != nil && evt.Response.Done {
			log.Printf("   ✅ [Event Data] Processing completed")
			break
		}
	}

	// Output statistics
	log.Println("   📊 [Event Statistics] Event data converted from LKE callbacks:")
	for eventType, count := range eventStats {
		if count > 0 {
			log.Printf("      %s: %d times", eventType, count)
		}
	}

	log.Println("   🔄 [Data Comparison] Check original sync.Map data:")
	time.Sleep(100 * time.Millisecond) // Ensure data write completion
	messages := GetAgentTask(sessionID + "-event-demo")
	for _, msg := range messages {
		log.Printf("      Original data %s: %s", msg.Type, msg.Content)
	}

	// Demonstrate how business code processes these events
	demonstrateBusinessEventProcessing(downloadAgent)
}

// demonstrateBusinessEventProcessing demonstrates how business code processes event data
func demonstrateBusinessEventProcessing(downloadAgent agent.Agent) {
	log.Println("\n   💼 [Business Processing] Demo how business code processes event data:")

	ctx := context.Background()
	invocation := &agent.Invocation{
		InvocationID: "business-demo",
		Message: model.Message{
			Content: "Business scenario: batch download files and notify user progress",
			Role:    model.RoleUser,
		},
	}

	eventChan, err := downloadAgent.Run(ctx, invocation)
	if err != nil {
		log.Printf("      ❌ Business processing execution failed: %v", err)
		return
	}

	// Simulate business logic: different processing based on event types
	progressCount := 0
	for evt := range eventChan {
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			choice := evt.Response.Choices[0]

			switch evt.Response.Object {
			case model.ObjectTypeToolResponse:
				if len(choice.Message.ToolCalls) > 0 {
					progressCount++
					// Business logic: tool call started, notify user progress
					log.Printf("      📈 [Business Logic] Progress update %d: executing %s",
						progressCount, choice.Message.ToolCalls[0].Function.Name)

					// Here you can:
					// 1. Send WebSocket notification to frontend
					// 2. Update database progress status
					// 3. Send progress notification to message queue

				} else if choice.Message.Role == model.RoleTool {
					// Business logic: tool execution completed, record results
					log.Printf("      ✅ [Business Logic] Task completed: %s", choice.Message.Content)

					// Here you can:
					// 1. Save download results to database
					// 2. Trigger subsequent business processes
					// 3. Send completion notification
				}

			case model.ObjectTypeChatCompletion:
				// Business logic: final reply, record conversation history
				log.Printf("      💾 [Business Logic] Save conversation record: %s", choice.Message.Content)
				// Save to conversation history table, send final notification, etc.
			}
		}

		if evt.Response != nil && evt.Response.Done {
			break
		}
	}

	log.Printf("      🎯 [Business Logic] Processing completed, handled %d progress events", progressCount)
}

// demonstrateDisableEventBypass demonstrates disabling event bypass functionality
func demonstrateDisableEventBypass() {
	log.Println("\n🚫 Demo: Disable Event Bypass (Only original handler works)")

	// Create existing handler
	existingHandler := &MyDownloadEventHandler{
		SessionID: "disable-bypass-session",
		UserID:    "test-user",
	}

	// Create LKE client
	lkeClient := lkesdk.NewLkeClient("test-bot-key", existingHandler)
	lkeClient.SetMock(true)

	// Add tools
	downloadTool := &DownloadFileTool{}
	functionTool, err := lketool.NewFunctionTool(
		downloadTool.GetName(),
		downloadTool.GetDescription(),
		downloadTool.Execute,
		downloadTool.GetParametersSchema(),
	)
	if err != nil {
		log.Printf("   ❌ Failed to create function tool: %v", err)
		return
	}
	lkeClient.AddFunctionTools("TestAgent", []*lketool.FunctionTool{functionTool})

	// Wrap as trpc-agent-go Agent with disabled event bypass
	testAgent := lkeagent.New(lkeClient,
		lkeagent.WithName("test-agent-no-bypass"),
		lkeagent.WithOriginalHandler(existingHandler),
		lkeagent.WithVisitorBizID("test-user"),
		lkeagent.WithBufferSize(10),
		lkeagent.WithEventBypass(false), // Disable event bypass
	)

	log.Println("   ✅ Agent created with EnableEventBypass=false")

	// Execute task
	ctx := context.Background()
	invocation := &agent.Invocation{
		InvocationID: "test-disable-bypass",
		Message: model.Message{
			Content: "Test with disabled bypass: download file",
			Role:    model.RoleUser,
		},
	}

	eventChan, err := testAgent.Run(ctx, invocation)
	if err != nil {
		log.Printf("   ❌ Execution failed: %v", err)
		return
	}

	log.Println("   📡 Monitoring event stream (should be empty due to disabled bypass)...")
	eventCount := 0

	for evt := range eventChan {
		if evt.Response != nil {
			eventCount++
			log.Printf("   📤 [Unexpected Event] Received: %s", evt.Response.Object)

			if evt.Response.Done {
				break
			}
		}
	}

	log.Printf("   📊 Total received %d events (expected: 0)", eventCount)
	log.Println("   ✅ Conclusion: With EnableEventBypass=false, no events sent to trpc-agent-go")
	log.Println("   💡 Only original handler logic works, sync.Map still gets data")

	// Check that original handler still worked
	time.Sleep(100 * time.Millisecond)
	messages := GetAgentTask("disable-bypass-session")
	log.Printf("   🔄 Original handler data: %d messages stored in sync.Map", len(messages))
}

// demonstrateNoOriginalHandler demonstrates the case without passing originalHandler
func demonstrateNoOriginalHandler() {
	log.Println("\n🧪 Demo: Case without passing originalHandler")

	// Create LKE client (pass nil as EventHandler)
	lkeClient := lkesdk.NewLkeClient("test-bot-key", nil)
	lkeClient.SetMock(true)

	// Add tools
	downloadTool := &DownloadFileTool{}
	functionTool, err := lketool.NewFunctionTool(
		downloadTool.GetName(),
		downloadTool.GetDescription(),
		downloadTool.Execute,
		downloadTool.GetParametersSchema(),
	)
	if err != nil {
		log.Printf("   ❌ Failed to create function tool: %v", err)
		return
	}
	lkeClient.AddFunctionTools("TestAgent", []*lketool.FunctionTool{functionTool})

	// Wrap as trpc-agent-go Agent using WithOption pattern
	testAgent := lkeagent.New(lkeClient,
		lkeagent.WithName("test-agent"),
		lkeagent.WithVisitorBizID("test-user"),
		lkeagent.WithBufferSize(10),
		lkeagent.WithEventBypass(true), // Explicitly enable event bypass
	)

	log.Println("   ✅ Agent created successfully (originalHandler=nil)")

	// Execute task
	ctx := context.Background()
	invocation := &agent.Invocation{
		InvocationID: "test-no-handler",
		Message: model.Message{
			Content: "Test no-handler scenario: download file",
			Role:    model.RoleUser,
		},
	}

	eventChan, err := testAgent.Run(ctx, invocation)
	if err != nil {
		log.Printf("   ❌ Execution failed: %v", err)
		return
	}

	log.Println("   📡 Listening to event stream (no original business logic, only trpc-agent-go events)...")
	eventCount := 0

	for evt := range eventChan {
		if evt.Response != nil {
			eventCount++
			if len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[0]
				switch evt.Response.Object {
				case model.ObjectTypeChatCompletion:
					log.Printf("   💬 [trpc-agent-go Event] Final reply: %s", choice.Message.Content)
				case model.ObjectTypeToolResponse:
					if len(choice.Message.ToolCalls) > 0 {
						log.Printf("   🛠️ [trpc-agent-go Event] Tool call: %s", choice.Message.ToolCalls[0].Function.Name)
					} else if choice.Message.Role == model.RoleTool {
						log.Printf("   📄 [trpc-agent-go Event] Tool result: %s", choice.Message.Content)
					}
				}
			}

			if evt.Response.Done {
				break
			}
		}
	}

	log.Printf("   📊 Total received %d events", eventCount)
	log.Println("   ✅ Conclusion: Still works normally when originalHandler=nil")
	log.Println("   💡 Difference: No original business logic (like sync.Map storage), only trpc-agent-go event stream")
}

// demonstrateRunnerMode demonstrates Runner mode (examples/runner)
func demonstrateRunnerMode(ctx context.Context, downloadAgent agent.Agent, userID string) {
	log.Println("\n1️⃣ Runner Mode Demo:")
	appRunner := runner.NewRunner("download-app", downloadAgent)

	// Execute a task
	message := model.NewUserMessage("Test Runner mode: download file to /tmp directory")
	eventChan, err := appRunner.Run(ctx, userID, "runner-session", message)
	if err != nil {
		log.Printf("   ❌ Runner execution failed: %v", err)
	} else {
		log.Printf("   ✅ Runner execution successful")
		// Consume result events (simplified processing)
		go func() {
			for range eventChan {
				// Consume events but don't process to avoid blocking
			}
		}()
		time.Sleep(500 * time.Millisecond) // Simple wait
		log.Printf("   📄 Runner task submitted")
	}
}

// demonstrateSubAgentMode demonstrates SubAgent mode (examples/transfer)
func demonstrateSubAgentMode(downloadAgent agent.Agent) {
	log.Println("\n2️⃣ SubAgent Mode Demo:")

	// Create coordinator Agent with download SubAgent
	modelInstance := openai.New("deepseek-chat")
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(1000),
		Temperature: floatPtr(0.6),
		Stream:      true,
	}

	coordinatorAgent := llmagent.New(
		"coordinator-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Coordinator Agent that can dispatch to specialized SubAgents like download"),
		llmagent.WithInstruction("When user needs to download files, use download sub-agent to handle"),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithSubAgents([]agent.Agent{downloadAgent}),
	)

	log.Printf("   ✅ Created coordinator Agent with SubAgent: %s", downloadAgent.Info().Name)
	_ = coordinatorAgent // Avoid unused warning
}

// demonstrateA2AMode demonstrates A2A Agent mode (examples/a2aagent)
func demonstrateA2AMode(downloadAgent agent.Agent) {
	log.Println("\n3️⃣ A2A Agent Mode Demo:")

	// Create A2A server
	server, err := a2a.New(
		a2a.WithHost("localhost:8888"),
		a2a.WithAgent(downloadAgent, true),
	)
	if err != nil {
		log.Printf("   ❌ Failed to create A2A server: %v", err)
	} else {
		log.Printf("   ✅ A2A server created successfully, accessible at localhost:8888")
		// In real applications, server.Start() would be called here
		_ = server
	}
}

// demonstrateMultiAgentMode demonstrates multi-agent collaboration mode (examples/multiagent)
func demonstrateMultiAgentMode(downloadAgent agent.Agent) {
	log.Println("\n4️⃣ Multi-Agent Collaboration Mode Demo:")

	// Create multiple Agent combinations
	agents := []agent.Agent{downloadAgent}
	log.Printf("   ✅ Can create Agent combinations: Chain, Parallel, Cycle, etc.")
	log.Printf("   📋 Current Agent list: %d Agents", len(agents))
	for i, a := range agents {
		log.Printf("      %d. %s - %s", i+1, a.Info().Name, a.Info().Description)
	}
}

// demonstrateExistingAPI verifies existing polling interface
func demonstrateExistingAPI(sessionID string) {
	log.Println("\n📊 Verify existing polling interface (business logic requires no modification):")
	time.Sleep(1 * time.Second) // Wait for event processing
	messages := GetAgentTask(sessionID)
	for _, msg := range messages {
		log.Printf("   📝 %s: %s", msg.Type, msg.Content)
	}
}

// ========== 🔧 Helper functions ==========

// intPtr returns int pointer
func intPtr(i int) *int {
	return &i
}

// floatPtr returns float64 pointer
func floatPtr(f float64) *float64 {
	return &f
}

// ========== 📦 Helper structures and functions ==========

// Simulate download_agent's core features - global data storage
var (
	UserSessionStorage sync.Map // Store all user session data, similar to download_agent
	UserSessionClients sync.Map // Store all clients, similar to download_agent
)

// Simulate download_agent's data structure
type AgentMsg struct {
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
}

// ExistingDownloadAgent represents a complete download_agent implementation using LKE SDK
type ExistingDownloadAgent struct {
	lkeClient    lkesdk.LkeClient
	eventHandler *MyDownloadEventHandler
}

func createExistingLKEAgent(sessionID, userID string) *ExistingDownloadAgent {
	log.Println("📦 Create existing download_agent (simulate existing LKE-based agent)...")

	// Existing EventHandler implementation
	eventHandler := &MyDownloadEventHandler{
		SessionID: sessionID,
		UserID:    userID,
	}

	// Create LKE client
	lkeClient := lkesdk.NewLkeClient("download-bot-key", eventHandler)
	lkeClient.SetMock(true) // Use Mock mode for demo

	// Add download tool
	downloadTool := &DownloadFileTool{}
	functionTool, err := lketool.NewFunctionTool(
		downloadTool.GetName(),
		downloadTool.GetDescription(),
		downloadTool.Execute,
		downloadTool.GetParametersSchema(),
	)
	if err != nil {
		log.Fatalf("Failed to create function tool: %v", err)
	}

	lkeClient.AddFunctionTools("DownloadAgent", []*lketool.FunctionTool{functionTool})
	log.Println("✅ Existing download_agent setup completed")

	return &ExistingDownloadAgent{
		lkeClient:    lkeClient,
		eventHandler: eventHandler,
	}
}

// MyDownloadEventHandler simulates MyEventHandler in download_agent
// This is the user's existing EventHandler implementation, no modification needed
type MyDownloadEventHandler struct {
	SessionID string
	UserID    string
}

func (h *MyDownloadEventHandler) OnReply(reply *lkeevent.ReplyEvent) {
	if !reply.IsFromSelf && reply.IsFinal {
		log.Printf("📁 [Existing Agent] Final reply: %s", reply.Content)

		// Existing business logic - store to global sync.Map for polling interface
		msg := &AgentMsg{
			Type:      "reply",
			Content:   reply.Content,
			Timestamp: time.Now(),
			SessionID: h.SessionID,
		}
		UserSessionStorage.Store(fmt.Sprintf("%s_reply", h.SessionID), msg)
	}
}

func (h *MyDownloadEventHandler) OnThought(thought *lkeevent.AgentThoughtEvent) {
	if len(thought.Procedures) > 0 {
		procedure := thought.Procedures[len(thought.Procedures)-1]
		log.Printf("🧠 [Existing Agent] Thought process: %s", procedure.Debugging.Content)

		// Existing business logic
		msg := &AgentMsg{
			Type:      "thought",
			Content:   procedure.Debugging.Content,
			Timestamp: time.Now(),
			SessionID: h.SessionID,
		}
		UserSessionStorage.Store(fmt.Sprintf("%s_thought", h.SessionID), msg)
	}
}

func (h *MyDownloadEventHandler) OnError(err *lkeevent.ErrorEvent) {
	log.Printf("❌ [Existing Agent] Error: %s", err.Error.Message)

	// Existing business logic
	msg := &AgentMsg{
		Type:      "error",
		Content:   err.Error.Message,
		Timestamp: time.Now(),
		SessionID: h.SessionID,
	}
	UserSessionStorage.Store(fmt.Sprintf("%s_error", h.SessionID), msg)
}

func (h *MyDownloadEventHandler) OnReference(refer *lkeevent.ReferenceEvent) {
	log.Printf("📚 [Existing Agent] Reference event")
}

func (h *MyDownloadEventHandler) OnTokenStat(stat *lkeevent.TokenStatEvent) {
	log.Printf("📊 [Existing Agent] Token statistics: used=%d, total=%d", stat.UsedCount, stat.TokenCount)
}

func (h *MyDownloadEventHandler) BeforeToolCallHook(toolCallCtx lkesdk.ToolCallContext) {
	log.Printf("🛠️ [Existing Agent] Start calling tool: %s", toolCallCtx.CallTool.GetName())

	// Existing business logic
	msg := &AgentMsg{
		Type:      "tool_call_start",
		Content:   fmt.Sprintf("Start calling tool: %s", toolCallCtx.CallTool.GetName()),
		Timestamp: time.Now(),
		SessionID: h.SessionID,
	}
	UserSessionStorage.Store(fmt.Sprintf("%s_tool_start", h.SessionID), msg)
}

func (h *MyDownloadEventHandler) AfterToolCallHook(toolCallCtx lkesdk.ToolCallContext) {
	if toolCallCtx.Err != nil {
		log.Printf("❌ [Existing Agent] Tool call failed: %s", toolCallCtx.Err.Error())
	} else {
		result := toolCallCtx.CallTool.ResultToString(toolCallCtx.Output)
		log.Printf("✅ [Existing Agent] Tool call succeeded: %s", result)
	}

	// Existing business logic
	content := "Tool call succeeded"
	if toolCallCtx.Err != nil {
		content = fmt.Sprintf("Tool call failed: %s", toolCallCtx.Err.Error())
	}
	msg := &AgentMsg{
		Type:      "tool_call_end",
		Content:   content,
		Timestamp: time.Now(),
		SessionID: h.SessionID,
	}
	UserSessionStorage.Store(fmt.Sprintf("%s_tool_end", h.SessionID), msg)
}

// GetAgentTask simulates existing polling interface (similar to download_agent's GetAgentTask)
func GetAgentTask(sessionID string) []*AgentMsg {
	var messages []*AgentMsg

	UserSessionStorage.Range(func(key, value interface{}) bool {
		keyStr := key.(string)
		if containsSession(keyStr, sessionID) {
			if msg, ok := value.(*AgentMsg); ok {
				messages = append(messages, msg)
			}
		}
		return true
	})

	return messages
}

func containsSession(key, sessionID string) bool {
	return len(key) >= len(sessionID) && key[:len(sessionID)] == sessionID
}

// DownloadFileTool simulates tools in download_agent
type DownloadFileTool struct{}

func (t *DownloadFileTool) GetName() string {
	return "download_file"
}

func (t *DownloadFileTool) GetDescription() string {
	return "Download specified file to local directory"
}

func (t *DownloadFileTool) GetParametersSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url":  map[string]interface{}{"type": "string", "description": "URL of the file to download"},
			"path": map[string]interface{}{"type": "string", "description": "Local path to save the file"},
		},
		"required": []string{"url", "path"},
	}
}

func (t *DownloadFileTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	url := params["url"].(string)
	path := params["path"].(string)

	// Simulate download process
	time.Sleep(100 * time.Millisecond)

	return fmt.Sprintf("Successfully downloaded file from %s to %s", url, path), nil
}

func (t *DownloadFileTool) ResultToString(result interface{}) string {
	return result.(string)
}

func (t *DownloadFileTool) GetTimeout() time.Duration {
	return 30 * time.Second
}

func (t *DownloadFileTool) SetTimeout(d time.Duration) {}
