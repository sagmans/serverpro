package httpjson

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestDefaultCheckRedirectRejectsNonHTTPS(t *testing.T) {
	first := httptest.NewRequest(http.MethodGet, "https://api.example.com/start", nil)
	next := httptest.NewRequest(http.MethodGet, "http://api.example.com/start", nil)
	if err := DefaultCheckRedirect(next, []*http.Request{first}); err == nil {
		t.Fatal("HTTP downgrade hop must be refused")
	}
}

func TestDefaultCheckRedirectRejectsCrossHostAndSubdomainHops(t *testing.T) {
	cases := []string{
		"https://evil.example.com/steal",
		"https://api.evil.example.com/steal",
		"https://api.example.com:8443/steal",
	}
	for _, target := range cases {
		first := httptest.NewRequest(http.MethodGet, "https://api.example.com/start", nil)
		next := httptest.NewRequest(http.MethodGet, target, nil)
		if err := DefaultCheckRedirect(next, []*http.Request{first}); err == nil {
			t.Fatalf("cross-host hop %q must be refused", target)
		}
	}
}

func TestDefaultCheckRedirectAllowsSameHostHTTPSWithCap(t *testing.T) {
	via := []*http.Request{httptest.NewRequest(http.MethodGet, "https://api.example.com/hop-0", nil)}
	for i := 1; i < maxRedirectHops; i++ {
		next := httptest.NewRequest(http.MethodGet, "https://api.example.com/hop-"+strconv.Itoa(i), nil)
		if err := DefaultCheckRedirect(next, via); err != nil {
			t.Fatalf("hop %d within cap refused: %v", i, err)
		}
		via = append(via, next)
	}
	overflow := httptest.NewRequest(http.MethodGet, "https://api.example.com/hop-final", nil)
	if err := DefaultCheckRedirect(overflow, via); err == nil {
		t.Fatalf("hop beyond cap of %d must be refused", maxRedirectHops)
	}
}

func TestDefaultCheckRedirectRefusesBodyReplayAfterRedirect(t *testing.T) {
	// The guard travels on every hop's context exactly as DoRaw attaches it upstream.
	guardCtx := context.WithValue(context.Background(), redirectGuardKey{}, &redirectGuard{hasBody: true})
	first := httptest.NewRequest(http.MethodPost, "https://api.example.com/create", nil).WithContext(guardCtx)
	replay := httptest.NewRequest(http.MethodPost, "https://api.example.com/create2", nil).WithContext(guardCtx)
	replay.Body = http.NoBody
	if err := DefaultCheckRedirect(replay, []*http.Request{first}); err == nil {
		t.Fatal("307/308 body replay across redirect must be refused")
	}
}

const mustNotReachTarget = "refused redirect target must not see any request"

func TestDoRawBlocksHTTPSDowngradeBeforeConnecting(t *testing.T) {
	hits := new(atomic.Int64)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()

	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+strings.TrimPrefix(plain.URL, "http://")+"/swap", http.StatusFound)
	}))
	defer tlsSrv.Close()

	_, _, err := Client{BaseURL: tlsSrv.URL, Token: "token", HTTP: tlsSrv.Client()}.DoRaw(context.Background(), http.MethodGet, "/start", nil, nil)
	if err == nil {
		t.Fatal("redirect into plaintext must be refused")
	}
	if hits.Load() != 0 {
		t.Errorf("%s (%d hits)", mustNotReachTarget, hits.Load())
	}
}

func TestDoRawFollowsSameHostRedirectAndKeepsAuthorization(t *testing.T) {
	sawAuth := new(atomic.Value)
	sawAuth.Store("")
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			sawAuth.Store(r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Redirect(w, r, "/final", http.StatusFound)
	}))
	defer tlsSrv.Close()

	body, _, err := Client{BaseURL: tlsSrv.URL, Token: "keepme", HTTP: tlsSrv.Client()}.DoRaw(context.Background(), http.MethodGet, "/start", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := sawAuth.Load().(string); string(body) != `{"ok":true}` || got != "Bearer keepme" {
		t.Fatalf("body/auth = %s/%q", body, got)
	}
}

func TestDoRawRefusesTempRedirectBodyReplayOnMutationEndpoint(t *testing.T) {
	requests := new(atomic.Int64)
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	// record keeps handler-goroutine failures off testing.T yet avoids the nil ResponseWriter
	// net/http.Error would receive from the shared recorder on assertion-only paths.
	record := func(w http.ResponseWriter, format string, args ...any) {
		handlerErr.Record(w, format, args...)
	}
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := requests.Add(1); n > 1 {
			body, _ := io.ReadAll(r.Body)
			record(w, "%s: replayed with body %q", mustNotReachTarget, string(body))
			return
		}
		http.Redirect(w, r, "/create-retry", http.StatusTemporaryRedirect)
	}))
	defer tlsSrv.Close()

	_, _, err := Client{BaseURL: tlsSrv.URL, Token: "token", HTTP: tlsSrv.Client()}.DoRaw(context.Background(), http.MethodPost, "/create", []byte(`{"secret":"bootstrap"}`), nil)
	if err == nil {
		t.Fatal("307/308 body replay must be refused instead of followed")
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("expected exactly one request, got %d", n)
	}
}

func TestDoRawStopsUnboundedSameHostRedirectLoop(t *testing.T) {
	steps := new(atomic.Int64)
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := steps.Add(1)
		http.Redirect(w, r, "/loop-"+strconv.Itoa(int(n%26)), http.StatusFound)
	}))
	defer tlsSrv.Close()

	_, _, err := Client{BaseURL: tlsSrv.URL, Token: "token", HTTP: tlsSrv.Client()}.DoRaw(context.Background(), http.MethodGet, "/loop-0", nil, nil)
	if err == nil {
		t.Fatal("redirect loop must stop at the policy cap")
	}
	if n := steps.Load(); n > int64(maxRedirectHops+1) {
		t.Fatalf("expected at most %d server requests, got %d", maxRedirectHops+1, n)
	}
}
