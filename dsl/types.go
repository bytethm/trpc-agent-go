//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
// Package dsl provides the DSL (Domain-Specific Language) for defining workflows.
// It allows users to define workflows in JSON format that can be compiled into
// executable trpc-agent-go StateGraphs.
package dsl

// Workflow represents a complete workflow definition in the engine DSL.
// It only contains fields that are required for execution by the graph
// engine and intentionally avoids any UI-specific concepts such as
// positions or visual layout information.
type Workflow struct {
	// Version is the DSL version (e.g., "1.0")
	Version string `json:"version"`

	// Name is the workflow name
	Name string `json:"name"`

	// Description describes what this workflow does
	Description string `json:"description,omitempty"`

	// Nodes are the component instances in this workflow
	Nodes []Node `json:"nodes"`

	// Edges define the connections between nodes
	Edges []Edge `json:"edges"`

	// ConditionalEdges define conditional routing between nodes
	ConditionalEdges []ConditionalEdge `json:"conditional_edges,omitempty"`

	// StateVariables declares workflow-level state variables that can be read
	// and written by nodes (for example via builtin.set_state). This allows
	// the workflow author to define the shape and reducer behavior of global
	// state independently of any particular component.
	StateVariables []StateVariable `json:"state_variables,omitempty"`

	// EntryPoint is the ID of the starting node
	EntryPoint string `json:"entry_point"`

	// Metadata contains additional workflow-level metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// StateVariable describes a workflow-level state variable. It is the single
// source of truth for the variable's existence and coarse-grained type; nodes
// such as builtin.set_state only assign to already-declared variables.
type StateVariable struct {
	// Name is the state field name (e.g., "greeting", "counter").
	Name string `json:"name"`

	// Kind is a coarse-grained classification for editor usage and schema
	// inference: "string", "number", "boolean", "object", "array", "opaque".
	// When omitted, it is treated as "opaque".
	Kind string `json:"kind,omitempty"`

	// JSONSchema optionally provides a JSON Schema when this variable
	// represents a structured object.
	JSONSchema map[string]any `json:"json_schema,omitempty"`

	// Description explains what this state variable is for.
	Description string `json:"description,omitempty"`

	// Default is the default value if the state field is not present when
	// the workflow starts. When omitted, no default is applied.
	Default any `json:"default,omitempty"`

	// Reducer specifies the reducer function name for this state field.
	// When empty, the framework's DefaultReducer is used.
	Reducer string `json:"reducer,omitempty"`
}

// Node represents an executable node in the engine DSL.
// It is a flat structure containing only execution semantics; UI concepts
// like position/labels live in the view-layer DSL (server/dsl).
type Node struct {
	// ID is the unique node identifier (e.g., "llm_node_1")
	ID string `json:"id"`

	// EngineNode fields are embedded so that JSON fields such as "component",
	// "config", "inputs", and "outputs" appear directly under the node.
	EngineNode `json:",inline"`
}

// EngineNode represents the engine-level node definition embedded under Node.Data.Engine.
// It closely mirrors the original Node structure before ReactFlow alignment.
type EngineNode struct {
	// Label is the human-readable label for this node instance
	Label string `json:"label,omitempty"`

	// NodeType specifies which component to use (e.g., "builtin.llmagent").
	NodeType string `json:"node_type"`

	// NodeVersion is an optional component version (mirrors ComponentRef.Version).
	NodeVersion string `json:"node_version,omitempty"`

	// Config contains component-specific configuration
	Config map[string]interface{} `json:"config,omitempty"`

	// Inputs defines the input parameters for this node (optional, overrides component metadata)
	Inputs []NodeIO `json:"inputs,omitempty"`

	// Outputs defines the output parameters for this node (optional, overrides component metadata)
	Outputs []NodeIO `json:"outputs,omitempty"`

	// Description describes what this node does
	Description string `json:"description,omitempty"`
}

// Edge represents a direct connection between two nodes.
// It aligns with ReactFlow's edge structure by using source/target.
type Edge struct {
	// ID is the unique edge identifier (optional, auto-generated if not provided)
	ID string `json:"id,omitempty"`

	// Source is the source node ID
	Source string `json:"source"`

	// Target is the target node ID
	Target string `json:"target"`

	// Label is the edge label for UI display
	Label string `json:"label,omitempty"`
}

// ConditionalEdge represents a conditional routing decision.
type ConditionalEdge struct {
	// ID is the unique conditional edge identifier
	ID string `json:"id,omitempty"`

	// From is the source node ID
	From string `json:"from"`

	// Condition specifies the routing logic
	Condition Condition `json:"condition"`

	// Label is the edge label for UI display
	Label string `json:"label,omitempty"`
}

// Condition defines the routing logic for conditional edges.
type Condition struct {
	// Type is the condition type: "builtin", "function", or "tool_routing"
	Type string `json:"type"`

	// Cases describes ordered builtin cases. The first matching case wins.
	// Only used when Type="builtin".
	Cases []BuiltinCase `json:"cases,omitempty"`

	// Default is the default route if no case matches (for builtin/function).
	Default string `json:"default,omitempty"`

	// Function is a custom routing function reference.
	// Only used when Type="function".
	Function string `json:"function,omitempty"`

	// Routes maps condition results to target node IDs.
	// Only used when Type="function".
	Routes map[string]string `json:"routes,omitempty"`

	// ToolsNode is the tools node ID for tool routing.
	// Only used when Type="tool_routing".
	ToolsNode string `json:"tools_node,omitempty"`

	// Fallback is the next node after tools decision/execution.
	// Only used when Type="tool_routing".
	Fallback string `json:"fallback,omitempty"`
}

// BuiltinCase represents a single builtin case branch in a conditional edge.
// Cases are evaluated in order; the first matching case's Target is chosen.
type BuiltinCase struct {
	// Name is an optional human-readable label for the case (UI/meta).
	Name string `json:"name,omitempty"`

	// Condition is the structured builtin condition to evaluate.
	Condition CaseCondition `json:"condition"`

	// Target is the node ID to route to when this case matches.
	Target string `json:"target"`
}

// CaseCondition represents a structured condition configuration.
// It contains multiple condition rules that are evaluated together using a logical operator.
type CaseCondition struct {
	// Conditions is the list of condition rules to evaluate
	Conditions []ConditionRule `json:"conditions"`

	// LogicalOperator specifies how to combine multiple conditions
	// Valid values: "and", "or"
	// Default: "and"
	LogicalOperator string `json:"logical_operator,omitempty"`
}

// ConditionRule represents a single condition rule.
// It compares a variable from state against a value using an operator.
type ConditionRule struct {
	// Variable is the path to the variable in state (e.g., "state.score", "state.category")
	Variable string `json:"variable"`

	// Operator is the comparison operator
	// Supported operators:
	//   String/Array: "contains", "not_contains", "starts_with", "ends_with",
	//                 "is", "is_not", "empty", "not_empty", "in", "not_in"
	//   Number: "==", "!=", ">", "<", ">=", "<="
	//   Null: "null", "not_null"
	Operator string `json:"operator"`

	// Value is the value to compare against
	// Can be string, number, boolean, or array
	Value interface{} `json:"value,omitempty"`
}

// NodeIO defines an input or output parameter for a node.
// It can override the component's default I/O schema and specify data sources/targets.
type NodeIO struct {
	// Name is the parameter name (e.g., "messages", "result")
	Name string `json:"name"`

	// Type is the parameter type string (historically Go type, e.g., "string",
	// "[]string", "model.Message"). New code should prefer TypeID/Kind for
	// frontend use.
	Type string `json:"type,omitempty"`

	// TypeID is the DSL-level type identifier exposed to frontends. For example:
	// "string", "number", "graph.messages", "llmagent.output_parsed".
	TypeID string `json:"type_id,omitempty"`

	// Kind is a coarse-grained classification for editor usage:
	// "string", "number", "boolean", "object", "array", "opaque".
	Kind string `json:"kind,omitempty"`

	// JSONSchema optionally provides a JSON Schema when this IO represents a
	// structured object. This mirrors ParameterSchema.JSONSchema and is mainly
	// intended for view-layer editors.
	JSONSchema map[string]any `json:"json_schema,omitempty"`

	// Required indicates if this parameter is required
	Required bool `json:"required,omitempty"`

	// Description explains what this parameter is for
	Description string `json:"description,omitempty"`

	// Source specifies where to read the input data from (only for inputs)
	Source *IOSource `json:"source,omitempty"`

	// Target specifies where to write the output data to (only for outputs)
	Target *IOTarget `json:"target,omitempty"`

	// Default is the default value if not provided
	Default interface{} `json:"default,omitempty"`

	// Reducer specifies the reducer function name for this output (only for outputs)
	// This is used when multiple parallel nodes write to the same state field
	Reducer string `json:"reducer,omitempty"`
}

// IOSource specifies where to read input data from.
type IOSource struct {
	// Type is the source type: "state", "node", "constant"
	Type string `json:"type"`

	// Field is the state field name (when Type="state")
	Field string `json:"field,omitempty"`

	// Node is the source node ID (when Type="node")
	Node string `json:"node,omitempty"`

	// Output is the output parameter name from the source node (when Type="node")
	Output string `json:"output,omitempty"`

	// Value is the constant value (when Type="constant")
	Value interface{} `json:"value,omitempty"`
}

// IOTarget specifies where to write output data to.
type IOTarget struct {
	// Type is the target type: "state", "output"
	Type string `json:"type"`

	// Field is the state field name (when Type="state")
	Field string `json:"field,omitempty"`
}
