// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
package dsl

import (
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/dsl/registry"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// Validator validates DSL workflows.
// It performs multi-level validation:
// 1. Structure validation (required fields, valid references)
// 2. Semantic validation (no cycles, reachable nodes)
// 3. Component validation (components exist in registry)
// 4. Type validation (config matches component schema)
type Validator struct {
	registry *registry.Registry
}

// NewValidator creates a new validator with the given component registry.
func NewValidator(reg *registry.Registry) *Validator {
	return &Validator{
		registry: reg,
	}
}

// Validate validates an engine-level workflow. It operates purely on the
// execution DSL and does not depend on any UI-specific concepts.
func (v *Validator) Validate(workflow *Workflow) error {
	if workflow == nil {
		return fmt.Errorf("workflow is nil")
	}
	if err := v.validateStructure(workflow); err != nil {
		return fmt.Errorf("structure validation failed: %w", err)
	}

	if err := v.validateStateVariables(workflow); err != nil {
		return fmt.Errorf("state variables validation failed: %w", err)
	}

	if err := v.validateComponents(workflow); err != nil {
		return fmt.Errorf("component validation failed: %w", err)
	}

	if err := v.validateTopology(workflow); err != nil {
		return fmt.Errorf("topology validation failed: %w", err)
	}

	return nil
}

// validateStructure validates the basic structure of the workflow.
func (v *Validator) validateStructure(workflow *Workflow) error {
	// Check version
	if workflow.Version == "" {
		return fmt.Errorf("workflow version is required")
	}

	// Check name
	if workflow.Name == "" {
		return fmt.Errorf("workflow name is required")
	}

	// Check nodes
	if len(workflow.Nodes) == 0 {
		return fmt.Errorf("workflow must have at least one node")
	}

	// Check for duplicate node IDs and component references. Track builtin.start
	// node (if present) so we can enforce related invariants.
	nodeIDs := make(map[string]bool)
	startNodeID := ""
	for _, node := range workflow.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node ID cannot be empty")
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("duplicate node ID: %s", node.ID)
		}
		nodeIDs[node.ID] = true

		engine := node.EngineNode

		// Validate node type reference.
		if engine.NodeType == "" {
			return fmt.Errorf("node %s: node_type is required", node.ID)
		}

		// Track builtin.start node. We only allow at most one such node.
		if engine.NodeType == "builtin.start" {
			if startNodeID != "" {
				return fmt.Errorf("multiple builtin.start nodes are not allowed (found %s and %s)", startNodeID, node.ID)
			}
			startNodeID = node.ID
		}
	}

	// Check entry point
	if workflow.EntryPoint == "" {
		return fmt.Errorf("workflow entry point is required")
	}
	if !nodeIDs[workflow.EntryPoint] {
		return fmt.Errorf("entry point %s does not exist", workflow.EntryPoint)
	}

	// If a builtin.start node is present, the workflow entry point must be that
	// node. The actual executable entry point will be derived from its outgoing
	// edge by the compiler.
	if startNodeID != "" && workflow.EntryPoint != startNodeID {
		return fmt.Errorf("workflow entry point must be builtin.start node %s when present (got %s)", startNodeID, workflow.EntryPoint)
	}

	// Validate edges
	startOutCount := 0
	for _, edge := range workflow.Edges {
		// Allow virtual Start and End nodes without explicit node definitions.
		if edge.Source != graph.Start && !nodeIDs[edge.Source] {
			return fmt.Errorf("edge %s: source node %s does not exist", edge.ID, edge.Source)
		}
		if edge.Target != graph.End && !nodeIDs[edge.Target] {
			return fmt.Errorf("edge %s: target node %s does not exist", edge.ID, edge.Target)
		}

		// Additional constraints for builtin.start (if present):
		if startNodeID != "" {
			if edge.Target == startNodeID {
				return fmt.Errorf("edge %s: builtin.start node %s cannot be the target of an edge", edge.ID, startNodeID)
			}
			if edge.Source == startNodeID {
				startOutCount++
			}
		}
	}

	if startNodeID != "" {
		if startOutCount == 0 {
			return fmt.Errorf("builtin.start node %s must have exactly one outgoing edge (found none)", startNodeID)
		}
		if startOutCount > 1 {
			return fmt.Errorf("builtin.start node %s must have exactly one outgoing edge (found %d)", startNodeID, startOutCount)
		}
	}

	// Validate conditional edges
	for _, condEdge := range workflow.ConditionalEdges {
		if !nodeIDs[condEdge.From] {
			return fmt.Errorf("conditional edge %s: source node %s does not exist", condEdge.ID, condEdge.From)
		}

		// Validate condition
		if condEdge.Condition.Type == "" {
			return fmt.Errorf("conditional edge %s: condition type is required", condEdge.ID)
		}

		switch condEdge.Condition.Type {
		case "tool_routing":
			// For tool_routing, validate tools_node and fallback.
			if condEdge.Condition.ToolsNode == "" {
				return fmt.Errorf("conditional edge %s: tools_node is required for tool_routing", condEdge.ID)
			}
			if !nodeIDs[condEdge.Condition.ToolsNode] {
				return fmt.Errorf("conditional edge %s: tools_node %s does not exist",
					condEdge.ID, condEdge.Condition.ToolsNode)
			}
			if condEdge.Condition.Fallback == "" {
				return fmt.Errorf("conditional edge %s: fallback is required for tool_routing", condEdge.ID)
			}
			if !nodeIDs[condEdge.Condition.Fallback] {
				return fmt.Errorf("conditional edge %s: fallback node %s does not exist",
					condEdge.ID, condEdge.Condition.Fallback)
			}
		case "builtin":
			// For builtin, ensure at least one case and each target exists.
			if len(condEdge.Condition.Cases) == 0 {
				return fmt.Errorf("conditional edge %s: builtin condition requires at least one case", condEdge.ID)
			}
			for idx, kase := range condEdge.Condition.Cases {
				if kase.Target == "" {
					return fmt.Errorf("conditional edge %s: builtin case %d target is empty", condEdge.ID, idx)
				}
				if !nodeIDs[kase.Target] {
					return fmt.Errorf("conditional edge %s: builtin case %d target node %s does not exist",
						condEdge.ID, idx, kase.Target)
				}
			}
			if condEdge.Condition.Default != "" && !nodeIDs[condEdge.Condition.Default] {
				return fmt.Errorf("conditional edge %s: default route target %s does not exist",
					condEdge.ID, condEdge.Condition.Default)
			}
		case "function":
			// For function conditions, validate routes map and default.
			if len(condEdge.Condition.Routes) == 0 {
				return fmt.Errorf("conditional edge %s: at least one route is required", condEdge.ID)
			}

			for routeKey, targetNode := range condEdge.Condition.Routes {
				if !nodeIDs[targetNode] {
					return fmt.Errorf("conditional edge %s: route %s target node %s does not exist",
						condEdge.ID, routeKey, targetNode)
				}
			}

			// Validate default route (if specified)
			if condEdge.Condition.Default != "" && !nodeIDs[condEdge.Condition.Default] {
				return fmt.Errorf("conditional edge %s: default route target %s does not exist",
					condEdge.ID, condEdge.Condition.Default)
			}
		default:
			return fmt.Errorf("conditional edge %s: unsupported condition type %s", condEdge.ID, condEdge.Condition.Type)
		}
	}

	return nil
}

// validateStateVariables validates workflow-level state variable declarations
// and ensures builtin.set_state assignments only target declared variables
// when declarations are present.
func (v *Validator) validateStateVariables(workflow *Workflow) error {
	declared := make(map[string]StateVariable)
	for idx, sv := range workflow.StateVariables {
		name := strings.TrimSpace(sv.Name)
		if name == "" {
			return fmt.Errorf("state_variables[%d]: name is required", idx)
		}
		if _, exists := declared[name]; exists {
			return fmt.Errorf("state_variables[%d]: duplicate state variable name %q", idx, name)
		}
		declared[name] = sv
	}

	// If no state variables are declared, we do not enforce assignments.
	if len(declared) == 0 {
		return nil
	}

	// Validate builtin.set_state assignments.
	for _, node := range workflow.Nodes {
		engine := node.EngineNode
		if engine.NodeType != "builtin.set_state" {
			continue
		}

		rawAssignments, ok := engine.Config["assignments"]
		if !ok || rawAssignments == nil {
			continue
		}

		assignSlice, ok := rawAssignments.([]any)
		if !ok {
			continue
		}

		for i, item := range assignSlice {
			assignMap, ok := item.(map[string]any)
			if !ok {
				continue
			}

			field, _ := assignMap["field"].(string)
			if strings.TrimSpace(field) == "" {
				// For compatibility with potential "name" field naming.
				field, _ = assignMap["name"].(string)
			}

			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}

			if _, exists := declared[field]; !exists {
				return fmt.Errorf("node %s: assignments[%d] field %q is not declared in workflow.state_variables", node.ID, i, field)
			}
		}
	}

	return nil
}

// validateComponents validates that all referenced components exist in the registry.
func (v *Validator) validateComponents(workflow *Workflow) error {
	for _, node := range workflow.Nodes {
		engine := node.EngineNode

		// Check if component exists
		if !v.registry.Has(engine.NodeType) {
			return fmt.Errorf("node %s: component %s not found in registry", node.ID, engine.NodeType)
		}

		// Get component metadata for validation
		metadata, err := v.registry.GetMetadata(engine.NodeType)
		if err != nil {
			return fmt.Errorf("node %s: failed to get component metadata: %w", node.ID, err)
		}

		// Validate config against component schema
		if err := v.validateConfig(node.ID, engine.Config, metadata.ConfigSchema); err != nil {
			return err
		}
	}

	return nil
}

// validateConfig validates a node's config against the component's config schema.
func (v *Validator) validateConfig(nodeID string, config map[string]interface{}, schema []registry.ParameterSchema) error {
	// Check required config parameters
	for _, param := range schema {
		if param.Required {
			if _, exists := config[param.Name]; !exists {
				return fmt.Errorf("node %s: required config parameter %s is missing", nodeID, param.Name)
			}
		}
	}

	// TODO: Add type validation for config values
	// This would require more sophisticated type checking

	return nil
}

// validateTopology validates the workflow topology (no unreachable nodes, etc.).
func (v *Validator) validateTopology(workflow *Workflow) error {
	// Build adjacency list
	adjacency := make(map[string][]string)
	for _, edge := range workflow.Edges {
		adjacency[edge.Source] = append(adjacency[edge.Source], edge.Target)
	}
	for _, condEdge := range workflow.ConditionalEdges {
		switch condEdge.Condition.Type {
		case "tool_routing":
			// tool_routing creates edges: from -> tools_node -> from -> fallback
			adjacency[condEdge.From] = append(adjacency[condEdge.From], condEdge.Condition.ToolsNode)
			adjacency[condEdge.Condition.ToolsNode] = append(adjacency[condEdge.Condition.ToolsNode], condEdge.From)
			adjacency[condEdge.From] = append(adjacency[condEdge.From], condEdge.Condition.Fallback)
		case "builtin":
			for _, kase := range condEdge.Condition.Cases {
				adjacency[condEdge.From] = append(adjacency[condEdge.From], kase.Target)
			}
			if condEdge.Condition.Default != "" {
				adjacency[condEdge.From] = append(adjacency[condEdge.From], condEdge.Condition.Default)
			}
		case "function":
			for _, target := range condEdge.Condition.Routes {
				adjacency[condEdge.From] = append(adjacency[condEdge.From], target)
			}
			if condEdge.Condition.Default != "" {
				adjacency[condEdge.From] = append(adjacency[condEdge.From], condEdge.Condition.Default)
			}
		}
	}

	// Find reachable nodes from entry point
	reachable := make(map[string]bool)
	v.dfs(workflow.EntryPoint, adjacency, reachable)

	// Check for unreachable nodes
	for _, node := range workflow.Nodes {
		if !reachable[node.ID] && node.ID != workflow.EntryPoint {
			return fmt.Errorf("node %s is unreachable from entry point", node.ID)
		}
	}

	return nil
}

// dfs performs depth-first search to find reachable nodes.
func (v *Validator) dfs(nodeID string, adjacency map[string][]string, visited map[string]bool) {
	if visited[nodeID] {
		return
	}
	visited[nodeID] = true

	for _, neighbor := range adjacency[nodeID] {
		v.dfs(neighbor, adjacency, visited)
	}
}
