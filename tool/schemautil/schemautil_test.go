package schemautil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestNormalize_NilSchema(t *testing.T) {
	normalized, err := Normalize(nil)
	require.NoError(t, err)
	require.Nil(t, normalized)
}

func TestNormalize_ObjectAddsEmptyProperties(t *testing.T) {
	normalized, err := Normalize(&tool.Schema{Type: "object"})
	require.NoError(t, err)
	require.Equal(t, "object", normalized["type"])
	require.Contains(t, normalized, "properties")

	props, ok := normalized["properties"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, props)
}

func TestNormalize_MarshalError(t *testing.T) {
	normalized, err := Normalize(&tool.Schema{
		Type:                 "object",
		AdditionalProperties: func() {},
	})
	require.Error(t, err)
	require.Nil(t, normalized)
}

func TestEmptyObject(t *testing.T) {
	empty := EmptyObject()
	require.Equal(t, "object", empty["type"])
	props, ok := empty["properties"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, props)
}
