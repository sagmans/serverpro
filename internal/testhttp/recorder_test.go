package testhttp

import (
	"net/http/httptest"
	"sync"
	"testing"
)

func TestRecorderCollectsConcurrentHandlerErrors(t *testing.T) {
	var recorder Recorder
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			recorder.Record(httptest.NewRecorder(), "unexpected request")
		}()
	}
	group.Wait()
	if err := recorder.Err(); err == nil {
		t.Fatal("concurrent errors were not recorded")
	}
}

func TestRecorderCheckAcceptsNoErrors(t *testing.T) {
	var recorder Recorder
	recorder.Check(t)
}

func TestNewHandlerErrorRecorderOwnsCheck(t *testing.T) {
	recorder := NewHandlerErrorRecorder(t)
	recorder.Check()
}
