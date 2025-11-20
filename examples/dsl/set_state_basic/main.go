package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"trpc.group/trpc-go/trpc-agent-go/agent/graphagent"
	"trpc.group/trpc-go/trpc-agent-go/dsl"
	"trpc.group/trpc-go/trpc-agent-go/dsl/registry"
	_ "trpc.group/trpc-go/trpc-agent-go/dsl/registry/builtin" // Register builtin components
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// Minimal example that shows how builtin.set_state assigns values to
// workflow-level state variables, which can then be consumed by downstream
// nodes (e.g., builtin.end).

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	fmt.Println("🚀 Set State DSL Example")
	fmt.Println("==================================================")
	fmt.Println()

	// Load workflow
	data, err := os.ReadFile("workflow.json")
	if err != nil {
		return fmt.Errorf("failed to read workflow.json: %w", err)
	}

	var wf dsl.Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return fmt.Errorf("failed to parse workflow.json: %w", err)
	}

	fmt.Printf("✅ Loaded workflow: %s\n", wf.Name)
	fmt.Printf("   Description: %s\n", wf.Description)
	fmt.Printf("   Nodes: %d\n", len(wf.Nodes))
	fmt.Println()

	// Compile workflow
	compiler := dsl.NewCompiler(registry.DefaultRegistry)
	compiledGraph, err := compiler.Compile(&wf)
	if err != nil {
		return fmt.Errorf("failed to compile workflow: %w", err)
	}
	fmt.Println("✅ Workflow compiled successfully")
	fmt.Println()

	// Create GraphAgent and Runner
	graphAgent, err := graphagent.New("set-state-basic", compiledGraph,
		graphagent.WithDescription("Demonstrates builtin.set_state assigning state variables"),
	)
	if err != nil {
		return fmt.Errorf("failed to create graph agent: %w", err)
	}

	appRunner := runner.NewRunner("set-state-basic-workflow", graphAgent)
	defer appRunner.Close()

	// Run a single example input
	userID := "demo-user"
	sessionID := "demo-session"
	input := "world from set_state_basic"

	fmt.Printf("🔄 Running workflow with input: %q\n\n", input)
	if err := executeWorkflow(ctx, appRunner, userID, sessionID, input); err != nil {
		return err
	}

	return nil
}

func executeWorkflow(ctx context.Context, appRunner runner.Runner, userID, sessionID, userInput string) error {
	msg := model.NewUserMessage(userInput)
	events, err := appRunner.Run(ctx, userID, sessionID, msg)
	if err != nil {
		return fmt.Errorf("failed to run workflow: %w", err)
	}

	var (
		greeting   string
		counter    any
		endOutput  map[string]any
		lastResult string
	)

	for ev := range events {
		if ev.Error != nil {
			return fmt.Errorf("workflow error: %s", ev.Error.Message)
		}

		if ev.StateDelta != nil {
			if raw, ok := ev.StateDelta["greeting"]; ok {
				var s string
				if err := json.Unmarshal(raw, &s); err == nil {
					greeting = s
				}
			}
			if raw, ok := ev.StateDelta["counter"]; ok {
				var v any
				if err := json.Unmarshal(raw, &v); err == nil {
					counter = v
				}
			}
			if raw, ok := ev.StateDelta["end_structured_output"]; ok {
				var v map[string]any
				if err := json.Unmarshal(raw, &v); err == nil {
					endOutput = v
				}
			}
			if raw, ok := ev.StateDelta[graph.StateKeyLastResponse]; ok {
				var s string
				if err := json.Unmarshal(raw, &s); err == nil {
					lastResult = s
				}
			}
		}
	}

	fmt.Println("📊 State Variables after builtin.set_state")
	fmt.Println("==================================================")
	fmt.Printf("greeting: %q\n", greeting)
	fmt.Printf("counter:  %#v\n", counter)
	fmt.Println()

	fmt.Println("📋 End Structured Output")
	fmt.Println("==================================================")
	if endOutput != nil {
		b, _ := json.MarshalIndent(endOutput, "", "  ")
		fmt.Println(string(b))
	} else if lastResult != "" {
		fmt.Println(lastResult)
	} else {
		fmt.Println("<none>")
	}
	fmt.Println()

	return nil
}

