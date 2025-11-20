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
	// Register the SetState component so that it can be referenced from DSL
	// workflows as "builtin.set_state".
	registry.MustRegister(&SetStateComponent{})
}

// SetStateComponent assigns values to existing workflow state variables.
// It evaluates a list of expressions and writes the results into the
// corresponding state fields. Variable declaration (type/default) is handled
// at the workflow/start level; this component is purely an assignment node.
//
// Current implementation:
//   - Each assignment.expr is treated as a JSON literal encoded as a string.
//   - The parsed JSON value is passed through renderStructuredTemplate so
//     string leaves may contain {{state.*}} / {{nodes.*}} placeholders.
//   - This is a placeholder for a future ExpressionEngine (e.g., CEL) and
//     allows the DSL shape to be stabilized ahead of the engine implementation.
type SetStateComponent struct{}

// setStateAssignmentConfig describes a single assignment in config.
// It is intentionally internal-only; the external DSL shape is plain JSON.
type setStateAssignmentConfig struct {
	Field string
	Expr  map[string]any
}

// Metadata describes the SetState component.
func (c *SetStateComponent) Metadata() registry.ComponentMetadata {
	assignmentsType := reflect.TypeOf([]map[string]any{})

	return registry.ComponentMetadata{
		Name:        "builtin.set_state",
		DisplayName: "Set State",
		Description: "Assign values to workflow state variables based on expressions",
		Category:    "Data",
		Version:     "1.0.0",

		Inputs:  []registry.ParameterSchema{},
		Outputs: []registry.ParameterSchema{},

		ConfigSchema: []registry.ParameterSchema{
			{
				Name:        "assignments",
				DisplayName: "Assignments",
				Description: "List of {field, expr} objects describing state updates",
				Type:        "[]map[string]any",
				TypeID:      "array",
				Kind:        "array",
				GoType:      assignmentsType,
				Required:    false,
			},
		},
	}
}

// Execute evaluates all configured assignments and returns a state delta
// containing the updated fields. If no assignments are configured, the state
// is left unchanged.
func (c *SetStateComponent) Execute(ctx context.Context, config registry.ComponentConfig, state graph.State) (any, error) {
	raw := config.Get("assignments")
	if raw == nil {
		return graph.State{}, nil
	}

	rawSlice, ok := raw.([]any)
	if !ok {
		// Be tolerant of mis-typed configs.
		return graph.State{}, nil
	}

	if len(rawSlice) == 0 {
		return graph.State{}, nil
	}

	stateDelta := graph.State{}

	for _, item := range rawSlice {
		assignMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		field, _ := assignMap["field"].(string)
		if field == "" {
			// For compatibility with potential "name" field naming.
			field, _ = assignMap["name"].(string)
		}
		if strings.TrimSpace(field) == "" {
			continue
		}

		rawExpr, ok := assignMap["expr"].(map[string]any)
		if !ok {
			continue
		}

		exprStr, _ := rawExpr["expression"].(string)
		if strings.TrimSpace(exprStr) == "" {
			continue
		}

		// Try to parse expression as JSON first.
		var parsed any
		if err := json.Unmarshal([]byte(exprStr), &parsed); err == nil {
			// Recursively render templates in the parsed JSON value.
			rendered := renderStructuredTemplate(parsed, state)
			stateDelta[field] = rendered
			continue
		}

		// Fallback: treat expression as a plain string template.
		renderedStr := renderHTTPTemplate(exprStr, state)
		stateDelta[field] = renderedStr
	}

	return stateDelta, nil
}

