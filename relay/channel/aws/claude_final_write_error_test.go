package aws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type finalEventFailWriter struct {
	gin.ResponseWriter
	match  string
	failed bool
}

func (w *finalEventFailWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.match) {
		w.failed = true
		return 0, errors.New("synthetic final event failure")
	}
	return w.ResponseWriter.Write(p)
}

// Exercise the existing shared-finalizer caller using the SDK's fake HTTP
// client. No network request, real credentials or AWS resource is used.
func TestAwsClaudeStreamFinalWriteFailurePropagates(t *testing.T) {
	for _, match := range []string{`"choices":[],"usage":`, "[DONE]"} {
		t.Run(match, func(t *testing.T) {
			var body bytes.Buffer
			for _, frame := range []string{
				`{"type":"message_start","message":{"id":"msg_fixture","model":"fixture","usage":{"input_tokens":7,"output_tokens":1}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer"}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
				`{"type":"message_stop"}`,
			} {
				require.NoError(t, writeAwsStreamEvent(&body, frame))
			}
			recorder := httptest.NewRecorder()
			c := newAwsTestContext(recorder, context.Background())
			writer := &finalEventFailWriter{ResponseWriter: c.Writer, match: match}
			c.Writer = writer
			a := &Adaptor{AwsReq: newAwsStreamInput(), AwsClient: newAwsTestClient(awsHTTPClientFunc(func(r *http.Request) (*http.Response, error) {
				return newAwsStreamResponse(r, io.NopCloser(bytes.NewReader(body.Bytes()))), nil
			}))}
			err, usage := awsStreamHandler(c, newAwsTestRelayInfo(), a)
			require.True(t, writer.failed)
			require.NotNil(t, err)
			assert.Nil(t, usage)
			assert.True(t, types.IsSkipRetryError(err))
			assert.NotContains(t, recorder.Body.String(), "[DONE]")
		})
	}
}
