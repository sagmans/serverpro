package vultr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestListCatalogReadsRegionsPlansAndOS(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != vultrCatalogPageSize || r.URL.Query().Get("page") != "" {
			handlerErr.Record(w, "bad pagination query %s", r.URL.RawQuery)
			return
		}
		switch r.URL.Path {
		case "/regions":
			_, _ = w.Write([]byte(`{"regions":[{"id":"ewr","city":"New York","country":"US","continent":"North America"}],"meta":{"links":{"next":"","prev":""}}}`))
		case "/plans":
			_, _ = w.Write([]byte(`{"plans":[{"id":"vc2-1c-2gb","vcpu_count":1,"ram":2048,"disk":55,"disk_count":1,"bandwidth":2048,"monthly_cost":5.0,"type":"vc2","locations":["ewr"]}],"meta":{"links":{"next":"","prev":""}}}`))
		case "/os":
			_, _ = w.Write([]byte(`{"os":[{"id":1743,"name":"Ubuntu 24.04 LTS x64","arch":"x64","family":"ubuntu"}],"meta":{"links":{"next":"","prev":""}}}`))
		default:
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	catalog, err := NewWithHTTP("token", ts.URL, ts.Client()).Catalog(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Regions) != 1 || catalog.Regions[0].ID != "ewr" {
		t.Fatalf("bad regions: %+v", catalog.Regions)
	}
	if len(catalog.Plans) != 1 || catalog.Plans[0].ID != "vc2-1c-2gb" {
		t.Fatalf("bad plans: %+v", catalog.Plans)
	}
	if len(catalog.OS) != 1 || catalog.OS[0].ID != 1743 {
		t.Fatalf("bad os: %+v", catalog.OS)
	}
}

func TestPlansAcceptsStringGPUCount(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plans" {
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
			return
		}
		// Vultr has returned gpu_count as a JSON string, so catalog reads must tolerate it.
		_, _ = w.Write([]byte(`{"plans":[{"id":"vc2-1c-2gb","vcpu_count":1,"ram":2048,"disk":55,"disk_count":1,"bandwidth":2048,"monthly_cost":5.0,"type":"vc2","locations":["ewr"],"gpu_count":"1"}],"meta":{"links":{"next":"","prev":""}}}`))
	}))
	defer ts.Close()

	plans, err := NewWithHTTP("token", ts.URL, ts.Client()).Plans(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].GPUCount != 1 {
		t.Fatalf("bad plans: %+v", plans)
	}
}

func TestPlansAcceptsFractionalStringGPUCount(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plans" {
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
			return
		}
		// Vultr shared GPU plans can report fractional GPU allocations like "1/8".
		_, _ = w.Write([]byte(`{"plans":[{"id":"vcg-a16-2c-2gb-1t","vcpu_count":2,"ram":2048,"disk":25,"disk_count":1,"bandwidth":1024,"monthly_cost":10.0,"type":"vcg","locations":["ewr"],"gpu_count":"1/8"}],"meta":{"links":{"next":"","prev":""}}}`))
	}))
	defer ts.Close()

	plans, err := NewWithHTTP("token", ts.URL, ts.Client()).Plans(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].GPUCount != 0 {
		t.Fatalf("bad plans: %+v", plans)
	}
}

func TestPlansAcceptsWholeRatioStringGPUCount(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plans" {
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
			return
		}
		// Whole ratios should keep whole GPU counts instead of being treated as fractional shares.
		_, _ = w.Write([]byte(`{"plans":[{"id":"vcg-a16-2c-2gb-1t","vcpu_count":2,"ram":2048,"disk":25,"disk_count":1,"bandwidth":1024,"monthly_cost":10.0,"type":"vcg","locations":["ewr"],"gpu_count":"2/1"}],"meta":{"links":{"next":"","prev":""}}}`))
	}))
	defer ts.Close()

	plans, err := NewWithHTTP("token", ts.URL, ts.Client()).Plans(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].GPUCount != 2 {
		t.Fatalf("bad plans: %+v", plans)
	}
}

func TestParseRatioInt(t *testing.T) {
	for _, tt := range []struct {
		name     string
		in       string
		want     int
		wantOkay bool
	}{
		{name: "fractional", in: "1/8", want: 0, wantOkay: true},
		{name: "zero numerator", in: "0/8", want: 0, wantOkay: true},
		{name: "whole ratio", in: "2/1", want: 2, wantOkay: true},
		{name: "divisible ratio", in: "16/8", want: 2, wantOkay: true},
		{name: "missing slash", in: "1", wantOkay: false},
		{name: "empty numerator", in: "/8", wantOkay: false},
		{name: "empty denominator", in: "1/", wantOkay: false},
		{name: "zero denominator", in: "1/0", wantOkay: false},
		{name: "negative numerator", in: "-1/8", wantOkay: false},
		{name: "negative denominator", in: "1/-8", wantOkay: false},
		{name: "non numeric", in: "one/eight", wantOkay: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOkay := parseRatioInt(tt.in)
			if got != tt.want || gotOkay != tt.wantOkay {
				t.Fatalf("parseRatioInt(%q) = (%d, %v), want (%d, %v)", tt.in, got, gotOkay, tt.want, tt.wantOkay)
			}
		})
	}
}

func TestListCatalogReadsPaginatedResponses(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/regions":
			_, _ = w.Write([]byte(`{"regions":[{"id":"ewr"}],"meta":{"links":{"next":"","prev":""}}}`))
		case "/plans":
			if r.URL.Query().Get("cursor") == "plans-next" {
				_, _ = w.Write([]byte(`{"plans":[{"id":"vc2-1c-2gb","vcpu_count":1,"ram":2048,"disk":55,"disk_count":1,"bandwidth":2048,"monthly_cost":5.0,"type":"vc2","locations":["ewr"]}],"meta":{"links":{"next":"","prev":""}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"plans":[],"meta":{"links":{"next":"plans-next","prev":""}}}`))
		case "/os":
			if r.URL.Query().Get("cursor") == "os-next" {
				_, _ = w.Write([]byte(`{"os":[{"id":1743,"name":"Ubuntu 24.04 LTS x64","arch":"x64","family":"ubuntu"}],"meta":{"links":{"next":"","prev":""}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"os":[],"meta":{"links":{"next":"os-next","prev":""}}}`))
		default:
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	catalog, err := NewWithHTTP("token", ts.URL, ts.Client()).Catalog(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Plans) != 1 || len(catalog.OS) != 1 {
		t.Fatalf("bad catalog: %+v", catalog)
	}
}

func TestListCatalogRejectsRepeatedCursor(t *testing.T) {
	requestCount := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plans" {
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
			return
		}
		requestCount++
		_, _ = w.Write([]byte(`{"plans":[],"meta":{"links":{"next":"same-cursor","prev":""}}}`))
	}))
	defer ts.Close()

	_, err := NewWithHTTP("token", ts.URL, ts.Client()).Plans(context.Background())
	handlerErr.Check()
	if err == nil || !strings.Contains(err.Error(), "repeated cursor") {
		t.Fatalf("expected repeated cursor error, got %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}
