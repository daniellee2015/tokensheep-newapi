package openai

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type responsesReadFailure struct{}

func (*responsesReadFailure) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func (*responsesReadFailure) Close() error { return nil }

func TestNativeResponsesReadFailureIsNotSuccess(t *testing.T) {
	c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "", relayconstant.RelayModeResponses)
	info.DisablePing = true
	resp.Body = &responsesReadFailure{}
	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.NotNil(t, err)
	assert.True(t, errors.Is(err.Err, io.ErrUnexpectedEOF))
	assert.Nil(t, usage)
	assert.Equal(t, relaycommon.StreamEndReasonScannerErr, info.StreamStatus.EndReason)
	assert.Empty(t, recorder.Body.String())
}

type responsesCancelOnFlushWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *responsesCancelOnFlushWriter) Flush() {
	w.ResponseWriter.Flush()
	w.cancel()
}

func TestNativeResponsesCancellationKeepsStatusWithoutSuccessTerminal(t *testing.T) {
	c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "", relayconstant.RelayModeResponses)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	c.Writer = &responsesCancelOnFlushWriter{ResponseWriter: c.Writer, cancel: cancel}
	info.IsStream, info.DisablePing = true, true
	pr, pw := io.Pipe()
	defer pw.Close()
	resp.Body = pr
	go func() {
		_, _ = io.WriteString(pw, "data: "+`{"type":"response.created","response":{"id":"resp_fixture","status":"in_progress"}}`+"\n\n")
	}()
	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err, "accounting retains the client_gone stream status")
	require.NotNil(t, usage)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	assert.NotContains(t, recorder.Body.String(), "response.completed")
}

func TestNativeResponsesPropagateDownstreamFailures(t *testing.T) {
	previous := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = previous })
	frames := []string{
		`{"type":"response.created","response":{"id":"resp_fixture","model":"fixture","status":"in_progress"}}`,
		`{"type":"response.output_text.delta","delta":"EARLY","item_id":"msg_fixture","output_index":0,"content_index":0}`,
		`{"type":"response.completed","response":{"id":"resp_fixture","model":"fixture","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11}}}`,
	}
	for _, match := range []string{"event: response.created", "EARLY", `data: {"type":"response.completed"`} {
		t.Run(match, func(t *testing.T) {
			c, recorder, info, resp := newUpstreamStreamErrorFixture(t, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n", relayconstant.RelayModeResponses)
			info.IsStream, info.DisablePing = true, true
			info.RelayFormat = types.RelayFormatOpenAIResponses
			c.Writer = &contentFlushFailWriter{ResponseWriter: c.Writer, match: match}
			usage, err := OaiResponsesStreamHandler(c, info, resp)
			require.NotNil(t, err, "a failed downstream write cannot return success")
			assert.Nil(t, usage)
			assert.True(t, types.IsSkipRetryError(err))
			assert.False(t, service.ShouldDisableChannel(err))
			assert.NotContains(t, recorder.Body.String(), `data: {"type":"response.completed"`)
			if match != "data: {\"type\":\"response.completed\"" {
				assert.True(t, info.ContentTiming.Snapshot().Downstream.FirstText.IsZero())
			}
		})
	}
}
