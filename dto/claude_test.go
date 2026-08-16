package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeRequestSetEffortPreservesOutputConfig(t *testing.T) {
	request := ClaudeRequest{
		OutputConfig: []byte(`{
			"format":{"type":"json_schema","schema":{"type":"object"}},
			"verbosity":"concise",
			"effort":"low"
		}`),
	}

	require.NoError(t, request.SetEffort("medium"))
	require.JSONEq(t, `{
		"format":{"type":"json_schema","schema":{"type":"object"}},
		"verbosity":"concise",
		"effort":"medium"
	}`, string(request.OutputConfig))
}

func TestClaudeRequestSetEffortInitializesEmptyOutputConfig(t *testing.T) {
	request := ClaudeRequest{}

	require.NoError(t, request.SetEffort("high"))
	require.JSONEq(t, `{"effort":"high"}`, string(request.OutputConfig))
}

func TestClaudeRequestSetEffortRejectsInvalidOutputConfig(t *testing.T) {
	request := ClaudeRequest{OutputConfig: []byte(`{"format":`)}

	require.ErrorContains(t, request.SetEffort("high"), "parse output_config")
}
