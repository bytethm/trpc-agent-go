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
	// Register the Transform component so that it can be referenced from DSL
	// workflows as "builtin.transform".
	registry.MustRegister(&TransformComponent{})
}

// TransformComponent is a data reshaping component. It evaluates an expression
// into a new structured object and writes it into state as a regular output
// field so downstream nodes can consume it.
//
// The current implementation treats the expression as a JSON literal encoded
// as a string and reuses the HTTP/template renderer for string leaves:
// string values may contain {{state.*}} / {{nodes.*}} placeholders. This is a
// placeholder for a future expression engine (e.g., CEL) and allows the DSL
// shape to be stabilized ahead of the engine implementation.
type TransformComponent struct{}

// Metadata describes the Transform component.
func (c *TransformComponent) Metadata() registry.ComponentMetadata {
	mapStringAnyType := reflect.TypeOf(map[string]any{})

	return registry.ComponentMetadata{
		Name:        "builtin.transform",
		DisplayName: "Transform",
		Description: "Reshape structured data into a new object based on an expression",
		Category:    "Data",
		Version:     "1.0.0",

		Inputs: []registry.ParameterSchema{},

		Outputs: []registry.ParameterSchema{
			{
				Name:        "result",
				DisplayName: "Transform Result",
				Description: "Structured object produced by this Transform node",
				Type:        "map[string]any",
				TypeID:      "transform.output",
				Kind:        "object",
				GoType:      mapStringAnyType,
				Required:    false,
			},
		},

		ConfigSchema: []registry.ParameterSchema{
			{
				Name:        "output_schema",
				DisplayName: "Output Schema",
				Description: "JSON Schema for the transformed output object (for tooling / UI)",
				Type:        "map[string]any",
				TypeID:      "object",
				Kind:        "object",
				GoType:      mapStringAnyType,
				Required:    false,
			},
			{
				Name:        "expr",
				DisplayName: "Transform Expression",
				Description: "Expression that evaluates to the transformed object. Currently expects a JSON string; string leaves may contain {{state.*}} / {{nodes.*}} placeholders.",
				Type:        "map[string]any",
				TypeID:      "object",
				Kind:        "object",
				GoType:      mapStringAnyType,
				Required:    false,
			},
		},
	}
}

// Execute evaluates the configured expression into a structured object and
// returns it under the "result" key in the state. If no expression is
// configured, the state is left unchanged.
func (c *TransformComponent) Execute(ctx context.Context, config registry.ComponentConfig, state graph.State) (any, error) {
	rawExpr := config.Get("expr")
	if rawExpr == nil {
		// No transform configured; leave state unchanged.
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

	// Parse the expression as JSON. This mirrors the current End component
	// behavior and is a stand-in for a future expression engine.
	var exprValue any
	if err := json.Unmarshal([]byte(exprStr), &exprValue); err != nil {
		// If parsing fails, be tolerant and leave state unchanged.
		return graph.State{}, nil
	}

	// Recursively render the parsed JSON value, supporting the same placeholder
	// syntax as builtin.http_request and builtin.end for string leaves.
	rendered := renderStructuredTemplate(exprValue, state)

	return graph.State{
		"result": rendered,
	}, nil
}

