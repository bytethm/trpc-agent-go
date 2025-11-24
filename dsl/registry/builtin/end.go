//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package builtin

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/dsl/registry"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

func init() {
	registry.MustRegister(&EndComponent{})
}

// EndComponent is a simple component that marks the logical end of a workflow
// branch. It does not modify state and can be used alongside the virtual
// graph.End node to provide a concrete "End" node in the DSL/UX.
type EndComponent struct{}

func (c *EndComponent) Metadata() registry.ComponentMetadata {
	mapStringAnyType := reflect.TypeOf(map[string]any{})

	return registry.ComponentMetadata{
		Name:        "builtin.end",
		DisplayName: "End",
		Description: "Marks the end of a workflow branch and can optionally set a structured final output",
		Category:    "Control",
		Version:     "1.0.0",

		Inputs: []registry.ParameterSchema{},

		Outputs: []registry.ParameterSchema{
			{
				Name:        "end_structured_output",
				DisplayName: "End Structured Output",
				Description: "Structured workflow output object set by this End node",
				Type:        "map[string]any",
				TypeID:      "end.structured_output",
				Kind:        "object",
				GoType:      mapStringAnyType,
				Required:    false,
			},
		},

		ConfigSchema: []registry.ParameterSchema{
			{
				Name:        "output_schema",
				DisplayName: "Output Schema",
				Description: "JSON Schema for the structured final output (for tooling / UI)",
				Type:        "map[string]any",
				TypeID:      "object",
				Kind:        "object",
				GoType:      mapStringAnyType,
				Required:    false,
			},
			{
				Name:        "expr",
				DisplayName: "Structured Output Expression",
				Description: "Expression that evaluates to the structured final output object (e.g., JSON literal). Currently expects a JSON string; string leaves may contain {{state.*}} / {{nodes.*}} placeholders.",
				Type:        "map[string]any",
				TypeID:      "object",
				Kind:        "object",
				GoType:      mapStringAnyType,
				Required:    false,
			},
		},
	}
}

func (c *EndComponent) Execute(ctx context.Context, config registry.ComponentConfig, state graph.State) (any, error) {
	rawExpr := config.Get("expr")
	if rawExpr == nil {
		// No structured output configured; leave state unchanged.
		return graph.State{}, nil
	}

	exprMap, ok := rawExpr.(map[string]any)
	if !ok {
		// Invalid expr type; ignore at runtime to be tolerant.
		return graph.State{}, nil
	}

	exprStr, _ := exprMap["expression"].(string)
	if strings.TrimSpace(exprStr) == "" {
		// Empty expression; leave state unchanged.
		return graph.State{}, nil
	}

	// Parse the expression as JSON. This mirrors OpenAI's pattern where
	// expression is typically a JSON literal encoded as a string.
	var exprValue any
	if err := json.Unmarshal([]byte(exprStr), &exprValue); err != nil {
		// If parsing fails, be tolerant and leave state unchanged.
		return graph.State{}, nil
	}

	// Recursively render the parsed JSON value, supporting the same placeholder
	// syntax as builtin.http_request for string leaves.
	rendered := renderStructuredTemplate(exprValue, state)

	// Try to record this structured output in the per-node cache as well,
	// so that downstream logic can access it via node_structured[<nodeID>].
	var nodeID string
	if nodeIDData, exists := state[graph.StateKeyCurrentNodeID]; exists {
		if id, ok := nodeIDData.(string); ok {
			nodeID = id
		}
	}

	stateDelta := graph.State{
		"end_structured_output": rendered,
	}

	if nodeID != "" {
		stateDelta["node_structured"] = map[string]any{
			nodeID: map[string]any{
				// For consistency with LLMAgent, we expose the End node's
				// structured result under the per-node "output_parsed" key.
				"output_parsed": rendered,
			},
		}
	}

	// Override last_response with a JSON string representation of the
	// structured final output so that existing runners / UIs that only look
	// at last_response can still see the End node's result, without relying
	// on any particular field name in the object.
	if b, err := json.Marshal(rendered); err == nil && strings.TrimSpace(string(b)) != "" {
		stateDelta[graph.StateKeyLastResponse] = string(b)
	}

	return stateDelta, nil
}

// renderStructuredTemplate walks an arbitrary JSON-like structure and renders
// string leaves using the HTTP template renderer ({{state.*}} / {{nodes.*}}).
func renderStructuredTemplate(value any, state graph.State) any {
	switch v := value.(type) {
	case string:
		return renderHTTPTemplate(v, state)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, vv := range v {
			out[k] = renderStructuredTemplate(vv, state)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, vv := range v {
			out[i] = renderStructuredTemplate(vv, state)
		}
		return out
	default:
		return value
	}
}
