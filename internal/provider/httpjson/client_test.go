package httpjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

const testResponseBodyLimit = 1 << 20

func TestDoRawSendsHeadersAndReturnsResponseHeaders(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Accept") != "application/json" || r.Header.Get("If-Match") != `"v1"` {
			handlerErr.Record(w, "headers = %#v", r.Header)
			return
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	body, header, err := Client{BaseURL: srv.URL, Token: "token", HTTP: srv.Client()}.DoRaw(context.Background(), http.MethodPost, "/policy", []byte(`{"a":1}`), http.Header{"Accept": {"application/json"}, "If-Match": {`"v1"`}})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"ok":true}` || header.Get("ETag") != `"v2"` {
		t.Fatalf("body/header = %s/%#v", body, header)
	}
}

func TestIsStatusRejectsNonStatusAndMismatchedCodes(t *testing.T) {
	if IsStatus(errors.New("plain error"), http.StatusNotFound) {
		t.Fatal("non-StatusError must not match a status code")
	}
	if IsStatus(&StatusError{StatusCode: http.StatusInternalServerError}, http.StatusNotFound) {
		t.Fatal("mismatched status code must not match")
	}
	if !IsStatus(&StatusError{StatusCode: http.StatusNotFound}, http.StatusNotFound) {
		t.Fatal("matching status code should match")
	}
}

func TestStatusErrorMessageIncludesMethodPathAndBody(t *testing.T) {
	err := &StatusError{Method: http.MethodGet, Path: "/x", Status: "404 Not Found", StatusCode: http.StatusNotFound, Body: "missing"}
	msg := err.Error()
	for _, want := range []string{http.MethodGet, "/x", "404 Not Found", "missing"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("status error message %q missing %q", msg, want)
		}
	}
}

func TestDoRawAcceptsCompleteBodyAtLimit(t *testing.T) {
	body := strings.Repeat("a", testResponseBodyLimit)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, _, err := Client{BaseURL: srv.URL, HTTP: srv.Client()}.DoRaw(context.Background(), http.MethodGet, "/limit", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body length = %d", len(got))
	}
}

func TestDoRawReturnsTypedErrorForOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", testResponseBodyLimit+1)))
	}))
	defer srv.Close()

	_, _, err := Client{BaseURL: srv.URL, HTTP: srv.Client()}.DoRaw(context.Background(), http.MethodGet, "/oversized", nil, nil)
	var tooLarge *BodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("error = %T %v, want *BodyTooLargeError", err, err)
	}
	if tooLarge.Limit != testResponseBodyLimit {
		t.Fatalf("limit = %d", tooLarge.Limit)
	}
	if !strings.Contains(err.Error(), strconv.FormatInt(testResponseBodyLimit, 10)) {
		t.Fatalf("error message = %q", err)
	}
}

func TestDoReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	err := Client{BaseURL: srv.URL, HTTP: srv.Client()}.Do(context.Background(), http.MethodGet, "/missing", nil, nil)
	if !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("expected 404 status error, got %v", err)
	}
}
