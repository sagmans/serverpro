package digitalocean

import (
	"fmt"
	"net/http"
	"testing"
)

type handlerErrorRecorder struct {
	t      *testing.T
	failed bool
}

func newHandlerErrorRecorder(t *testing.T) *handlerErrorRecorder {
	t.Helper()
	return &handlerErrorRecorder{t: t}
}

func (r *handlerErrorRecorder) record(w http.ResponseWriter, format string, args ...any) {
	r.t.Helper()
	r.failed = true
	http.Error(w, fmt.Sprintf(format, args...), http.StatusBadRequest)
}

func (r *handlerErrorRecorder) check() {
	r.t.Helper()
	if r.failed {
		r.t.Fatal("handler recorded unexpected request")
	}
}
