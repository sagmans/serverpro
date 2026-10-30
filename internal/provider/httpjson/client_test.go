package httpjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

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

	// A Cloudflare failure names the endpoint without the account or tunnel
	// it targeted, and repeats only the start of a long answer.
	const account, tunnel = "0123456789abcdef0123456789ABCDEF", "c0ffee00-1234-4abc-9def-0123456789ab"
	body := `{"success":false,"errors":[{"code":1000,"message":"tunnel ` + tunnel + ` failed"}],"result":"` + strings.Repeat("é", 400) + `"}`
	err = &StatusError{Method: http.MethodGet, Path: "/accounts/" + account + "/cfd_tunnel/" + tunnel + "/configurations?name=web", Status: "500 Internal Server Error", StatusCode: http.StatusInternalServerError, Body: body}
	msg = err.Error()
	for _, leaked := range []string{account, tunnel, "name=web"} {
		if strings.Contains(msg, leaked) {
			t.Fatalf("status error message %q repeats %q", msg, leaked)
		}
	}
	for _, want := range []string{"/accounts/{id}/cfd_tunnel/{id}/configurations failed", `"code":1000`, "tunnel {id} failed", "(" + strconv.Itoa(len(body)) + " bytes)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("status error message %q missing %q", msg, want)
		}
	}
	if !utf8.ValidString(msg) || len(msg) > len(http.MethodGet)+len(err.Path)+len(err.Status)+maxErrorBodyBytes+64 {
		t.Fatalf("status error message is not short valid UTF-8: %d bytes", len(msg))
	}
	if err.Body != body || !strings.Contains(err.Path, tunnel) {
		t.Fatal("the error's fields must keep the full path and body for classification")
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
