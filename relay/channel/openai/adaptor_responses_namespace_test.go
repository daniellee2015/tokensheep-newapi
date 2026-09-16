package openai

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestRestoresUniqueFunctionCallNamespace(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Tools: json.RawMessage(`[
			{"type":"function","name":"spawn_agent","namespace":"collaboration"},
			{"type":"function","name":"exec","namespace":"functions"}
		]`),
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"continue"},
			{"type":"function_call","call_id":"call_1","name":"spawn_agent","arguments":"{}"},
			{"type":"function_call","call_id":"call_2","name":"exec","namespace":"functions","arguments":"{}"}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, request)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	require.JSONEq(t, `[
		{"type":"message","role":"user","content":"continue"},
		{"type":"function_call","call_id":"call_1","name":"spawn_agent","namespace":"collaboration","arguments":"{}"},
		{"type":"function_call","call_id":"call_2","name":"exec","namespace":"functions","arguments":"{}"}
	]`, string(got.Input))
}

func TestConvertOpenAIResponsesRequestDoesNotGuessAmbiguousNamespace(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-6-astra",
		Tools: json.RawMessage(`[
			{"type":"function","name":"send_message","namespace":"collaboration"},
			{"type":"function","name":"send_message","namespace":"functions"},
			{"type":"function","name":"plain_tool"}
		]`),
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"send_message","arguments":"{}"},
			{"type":"function_call","call_id":"call_2","name":"plain_tool","arguments":"{}"},
			{"type":"function_call","call_id":"call_3","name":"unknown_tool","arguments":"{}"}
		]`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, request)
	require.NoError(t, err)
	got := converted.(dto.OpenAIResponsesRequest)
	require.JSONEq(t, string(request.Input), string(got.Input))
}
