package digitalocean

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestClientCatalogReadsRegionsSizesAndImages(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			handlerErr.Record(w, "authorization header missing")
			return
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != digitalOceanCatalogPageSize {
			handlerErr.Record(w, "bad pagination query %s", r.URL.RawQuery)
			return
		}
		switch r.URL.Path {
		case "/regions":
			_, _ = w.Write([]byte(`{"regions":[{"slug":"nyc3","name":"New York 3","available":true,"sizes":["s-1vcpu-1gb"]}],"links":{"pages":{}},"meta":{"total":1}}`))
		case "/sizes":
			_, _ = w.Write([]byte(`{"sizes":[{"slug":"s-1vcpu-1gb","memory":1024,"vcpus":1,"disk":25,"transfer":1,"price_monthly":6,"description":"Basic","regions":["nyc3"],"available":true}],"links":{"pages":{}},"meta":{"total":1}}`))
		case "/images":
			if r.URL.Query().Get("type") != "distribution" {
				handlerErr.Record(w, "image type query missing %s", r.URL.RawQuery)
				return
			}
			_, _ = w.Write([]byte(`{"images":[{"id":123,"slug":"ubuntu-24-04-x64","name":"24.04 (LTS) x64","distribution":"Ubuntu","regions":["nyc3"],"status":"available","public":true,"type":"base"}],"links":{"pages":{}},"meta":{"total":1}}`))
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
	if len(catalog.Regions) != 1 || catalog.Regions[0].Slug != "nyc3" {
		t.Fatalf("bad regions: %+v", catalog.Regions)
	}
	if len(catalog.Sizes) != 1 || catalog.Sizes[0].Slug != "s-1vcpu-1gb" {
		t.Fatalf("bad sizes: %+v", catalog.Sizes)
	}
	if len(catalog.Images) != 1 || catalog.Images[0].Slug != "ubuntu-24-04-x64" {
		t.Fatalf("bad images: %+v", catalog.Images)
	}
}

func TestClientCatalogReadsPaginatedResponses(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/regions":
			_, _ = w.Write([]byte(`{"regions":[{"slug":"nyc3","name":"New York 3","available":true}],"links":{"pages":{}},"meta":{"total":1}}`))
		case "/sizes":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"sizes":[{"slug":"s-1vcpu-1gb","memory":1024,"vcpus":1,"disk":25,"regions":["nyc3"],"available":true}],"links":{"pages":{}},"meta":{"total":1}}`))
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"sizes":[],"links":{"pages":{"next":%q}},"meta":{"total":1}}`, nextPageURL("/sizes", 2))))
		case "/images":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"images":[{"id":123,"slug":"ubuntu-24-04-x64","name":"24.04 (LTS) x64","distribution":"Ubuntu","regions":["nyc3"],"status":"available","public":true,"type":"base"}],"links":{"pages":{}},"meta":{"total":1}}`))
				return
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"images":[],"links":{"pages":{"next":%q}},"meta":{"total":1}}`, nextPageURL("/images", 2))))
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
	if len(catalog.Sizes) != 1 || len(catalog.Images) != 1 {
		t.Fatalf("bad catalog: %+v", catalog)
	}
}

func TestClientCatalogRejectsRepeatedPage(t *testing.T) {
	requestCount := 0
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sizes" {
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
			return
		}
		requestCount++
		_, _ = w.Write([]byte(fmt.Sprintf(`{"sizes":[],"links":{"pages":{"next":%q}},"meta":{"total":1}}`, nextPageURL("/sizes", 1))))
	}))
	defer ts.Close()

	_, err := NewWithHTTP("token", ts.URL, ts.Client()).Sizes(context.Background())
	handlerErr.Check()
	if err == nil || !strings.Contains(err.Error(), "repeated page") {
		t.Fatalf("expected repeated page error, got %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestNextPageRejectsMalformedNonEmptyLinks(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "malformed URL", raw: "://"},
		{name: "missing page", raw: "https://api.digitalocean.com/v2/sizes?per_page=200"},
		{name: "non-integer page", raw: "https://api.digitalocean.com/v2/sizes?page=next"},
		{name: "zero page", raw: "https://api.digitalocean.com/v2/sizes?page=0"},
		{name: "negative page", raw: "https://api.digitalocean.com/v2/sizes?page=-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, err := nextPage(test.raw)
			if err == nil || page != nil {
				t.Fatalf("page=%v err=%v", page, err)
			}
		})
	}
}

func TestComputeProviderCatalogMapsAvailableDropletOptions(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	defer handlerErr.Check()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/regions":
			_, _ = w.Write([]byte(`{"regions":[{"slug":"nyc3","name":"New York 3","available":true,"sizes":["s-1vcpu-1gb"]},{"slug":"nyc1","name":"New York 1","available":false}],"links":{"pages":{}},"meta":{"total":2}}`))
		case "/sizes":
			_, _ = w.Write([]byte(`{"sizes":[{"slug":"s-1vcpu-1gb","memory":1024,"vcpus":1,"disk":25,"price_monthly":6,"description":"Basic","regions":["nyc3"],"available":true},{"slug":"s-1vcpu-2gb","memory":2048,"vcpus":1,"disk":50,"regions":["sfo3"],"available":true},{"slug":"s-8vcpu-16gb-amd","memory":16384,"vcpus":8,"disk":100,"regions":["nyc3"],"available":false}],"links":{"pages":{}},"meta":{"total":3}}`))
		case "/images":
			_, _ = w.Write([]byte(`{"images":[{"id":123,"slug":"ubuntu-24-04-x64","name":"24.04 (LTS) x64","distribution":"Ubuntu","regions":["nyc3"],"status":"available","public":true,"type":"base"},{"id":124,"slug":"ubuntu-22-04-x64","name":"22.04 (LTS) x64","distribution":"Ubuntu","regions":["sfo3"],"status":"available","public":true,"type":"base"},{"id":125,"slug":"fedora-40-x64","name":"Fedora 40 x64","distribution":"Fedora","regions":["nyc3"],"status":"retired","public":true,"type":"base"}],"links":{"pages":{}},"meta":{"total":3}}`))
		default:
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	catalog, diagnostics := provider.Catalog(context.Background(), compute.CatalogQuery{Account: compute.Account{Token: "token"}, Location: "nyc3"})
	if !diagnostics.Passed() {
		t.Fatalf("catalog diagnostics failed: %v", diagnostics.Err())
	}
	if len(catalog.Locations) != 1 || catalog.Locations[0].Name != "nyc3" {
		t.Fatalf("bad locations: %+v", catalog.Locations)
	}
	if len(catalog.Sizes) != 1 || catalog.Sizes[0].Name != "s-1vcpu-1gb" {
		t.Fatalf("bad sizes: %+v", catalog.Sizes)
	}
	if len(catalog.Images) != 1 || catalog.Images[0].Name != "ubuntu-24-04-x64" {
		t.Fatalf("bad images: %+v", catalog.Images)
	}
}

func TestComputeProviderDoctorRedactsToken(t *testing.T) {
	// Build fixture token at runtime so static scanners do not treat it as a real credential.
	token := strings.Join([]string{"provider", "token", "value"}, "-")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid token "+token, http.StatusUnauthorized)
	}))
	defer srv.Close()

	provider := NewComputeProvider(func(token string) Client { return NewWithHTTP(token, srv.URL, srv.Client()) })
	diagnostics := provider.Doctor(context.Background(), compute.Account{Token: token})
	if diagnostics.Passed() || diagnostics.Err() == nil {
		t.Fatal("expected diagnostics failure")
	}
	if strings.Contains(diagnostics.Err().Error(), token) {
		t.Fatalf("token leaked in diagnostics: %v", diagnostics.Err())
	}
}

func nextPageURL(path string, page int) string {
	return fmt.Sprintf("https://api.digitalocean.com/v2%s?page=%d&per_page=%s", path, page, digitalOceanCatalogPageSize)
}
