package dsl

import (
	"fmt"
)

// expandWhile flattens builtin.while nodes in the given workflow into regular
// nodes and edges, while computing the metadata needed to add conditional
// back-edges in the StateGraph. The returned workflow does not contain any
// builtin.while nodes; instead, their body nodes are merged into the top
// level.
func (c *Compiler) expandWhile(workflow *Workflow) (*Workflow, map[string]*whileExpansion, error) {
	if workflow == nil {
		return nil, nil, fmt.Errorf("workflow is nil")
	}

	// Build an index of existing node IDs so that while bodies can be checked
	// for ID conflicts.
	nodeIDs := make(map[string]bool, len(workflow.Nodes))
	for _, n := range workflow.Nodes {
		nodeIDs[n.ID] = true
	}

	whileMeta := make(map[string]*whileExpansion)

	// Preprocess all builtin.while nodes and build their expansions.
	for _, n := range workflow.Nodes {
		if n.EngineNode.NodeType != "builtin.while" {
			continue
		}

		exp, err := c.buildWhileExpansion(n, workflow.Edges, nodeIDs)
		if err != nil {
			return nil, nil, fmt.Errorf("while node %s: %w", n.ID, err)
		}
		whileMeta[n.ID] = exp

		// Reserve body node IDs globally so subsequent while nodes cannot
		// accidentally reuse them.
		for _, bodyNode := range exp.BodyNodes {
			nodeIDs[bodyNode.ID] = true
		}
	}

	// If there are no while nodes, we can return the original workflow as-is.
	if len(whileMeta) == 0 {
		return workflow, whileMeta, nil
	}

	// Build the expanded workflow: shallow-copy workflow-level metadata and
	// conditional edges, but replace Nodes/Edges with the flattened view.
	expanded := *workflow
	expanded.Nodes = nil
	expanded.Edges = nil
	// Make a shallow copy of conditional edges slice so we can append body
	// edges without mutating the original workflow.
	if len(workflow.ConditionalEdges) > 0 {
		expanded.ConditionalEdges = append([]ConditionalEdge(nil), workflow.ConditionalEdges...)
	}

	// First, copy all non-while nodes.
	for _, n := range workflow.Nodes {
		if n.EngineNode.NodeType == "builtin.while" {
			continue
		}
		expanded.Nodes = append(expanded.Nodes, n)
	}
	// Then append all while body nodes.
	for _, exp := range whileMeta {
		expanded.Nodes = append(expanded.Nodes, exp.BodyNodes...)
	}

	// Rewire edges:
	//   - edge targeting while node -> edge to body entry
	//   - edge originating from while node -> omitted (replaced by cond edge)
	//   - all other edges are kept as-is
	for _, e := range workflow.Edges {
		if exp, ok := whileMeta[e.Target]; ok {
			newEdge := e
			newEdge.Target = exp.BodyEntry
			expanded.Edges = append(expanded.Edges, newEdge)
			continue
		}
		if _, ok := whileMeta[e.Source]; ok {
			// Outgoing edges from the while node are replaced by a
			// conditional edge from BodyExit, so skip the static edge.
			continue
		}
		expanded.Edges = append(expanded.Edges, e)
	}

	// Finally, add all internal edges from while bodies.
	for _, exp := range whileMeta {
		expanded.Edges = append(expanded.Edges, exp.BodyEdges...)
	}

	// And append conditional edges defined inside while bodies so they are
	// treated the same as top-level conditional edges during compilation.
	for _, exp := range whileMeta {
		if len(exp.BodyConditionalEdges) == 0 {
			continue
		}
		expanded.ConditionalEdges = append(expanded.ConditionalEdges, exp.BodyConditionalEdges...)
	}

	return &expanded, whileMeta, nil
}
