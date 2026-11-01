package common

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingEventWriter struct {
	*httptest.ResponseRecorder
	failAt, writes int
}

func (w *failingEventWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, errors.New("synthetic write failure")
	}
	return w.ResponseRecorder.Write(p)
}
func TestCustomEventPropagatesStreamWriteFailure(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		w := &failingEventWriter{ResponseRecorder: httptest.NewRecorder(), failAt: failAt}
		err := (CustomEvent{Data: `data: {"choices":[]}`}).Render(w)
		require.Error(t, err, "payload and terminator write failures must be observable")
		assert.Equal(t, failAt, w.writes, "must stop at the failed write")
	}
}
