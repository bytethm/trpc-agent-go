package schemautil

import (
	"encoding/json"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// EmptyObject returns a JSON Schema object describing an object with no properties.
func EmptyObject() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

// Normalize converts a *tool.Schema into a JSON-serializable object (map form) and applies
// minimal normalization for function-calling/tooling use-cases.
//
// It returns (nil, nil) when schema is nil.
func Normalize(schema *tool.Schema) (map[string]any, error) {
	if schema == nil {
		return nil, nil
	}
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(schemaBytes, &out); err != nil {
		return nil, err
	}
	NormalizeMapInPlace(out)
	return out, nil
}

// NormalizeMapInPlace applies best-effort normalization rules to an already-decoded JSON Schema.
func NormalizeMapInPlace(schema map[string]any) {
	if schema == nil {
		return
	}
	// Some function-calling implementations are strict about top-level object schemas having
	// an explicit `properties` key, even for no-arg tools.
	if typ, ok := schema["type"].(string); ok && typ == "object" {
		if props, exists := schema["properties"]; !exists || props == nil {
			schema["properties"] = map[string]any{}
		}
	}
}
