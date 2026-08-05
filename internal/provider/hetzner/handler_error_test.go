package hetzner

import (
	"fmt"
	"net/http"
	"testing"
)

type handlerErrorRecorder struct {
	t  *testing.T
	ch chan error
}

func newHandlerErrorRecorder(t *testing.T) *handlerErrorRecorder {
	return &handlerErrorRecorder{t: t, ch: make(chan error, 1)}
}

func (r *handlerErrorRecorder) record(w http.ResponseWriter, format string, args ...any) {
	select {
	case r.ch <- fmt.Errorf(format, args...):
	default:
	}
	http.Error(w, "unexpected test request", http.StatusBadRequest)
}

func (r *handlerErrorRecorder) check() {
	r.t.Helper()
	select {
	case err := <-r.ch:
		r.t.Fatal(err)
	default:
	}
}
