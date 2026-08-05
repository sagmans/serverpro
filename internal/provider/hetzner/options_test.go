package hetzner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagmans/serverpro/internal/testhttp"
)

func TestListOptionsParsesHetznerMetadata(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/locations":
			_, _ = w.Write([]byte(`{"locations":[{"id":1,"name":"fsn1","description":"Falkenstein DC Park 1","country":"DE","city":"Falkenstein","network_zone":"eu-central"}]}`))
		case "/server_types":
			_, _ = w.Write([]byte(`{"server_types":[{"id":45,"name":"cax11","description":"CAX11","category":"shared vCPU","cores":2,"memory":4,"disk":40,"cpu_type":"shared","architecture":"arm","locations":[{"name":"fsn1"}]}]}`))
		case "/images":
			if r.URL.Query().Get("type") != "system" || r.URL.Query().Get("status") != "available" {
				handlerErr.Record(w, "missing image filters: %s", r.URL.RawQuery)
				return
			}
			_, _ = w.Write([]byte(`{"images":[{"id":1,"type":"system","status":"available","name":"ubuntu-24.04","description":"Ubuntu 24.04","architecture":"arm","os_flavor":"ubuntu","os_version":"24.04","deprecated":null}]}`))
		default:
			handlerErr.Record(w, "unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewWithHTTP("token", ts.URL, ts.Client())
	locations, err := client.Locations(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	serverTypes, err := client.ServerTypes(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	images, err := client.Images(context.Background())
	handlerErr.Check()
	if err != nil {
		t.Fatal(err)
	}
	if locations[0].Name != "fsn1" || locations[0].NetworkZone != "eu-central" {
		t.Fatalf("bad locations: %+v", locations)
	}
	if serverTypes[0].Name != "cax11" || serverTypes[0].Architecture != "arm" || !serverTypes[0].SupportsLocation("fsn1") {
		t.Fatalf("bad server types: %+v", serverTypes)
	}
	if images[0].Name != "ubuntu-24.04" || images[0].Architecture != "arm" {
		t.Fatalf("bad images: %+v", images)
	}
}

func TestListOptionsReadsPaginatedResponses(t *testing.T) {
	handlerErr := testhttp.NewHandlerErrorRecorder(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/locations":
			_, _ = w.Write([]byte(`{"locations":[{"name":"fsn1"}],"meta":{"pagination":{"next_page":null}}}`))
		case "/server_types":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"server_types":[{"name":"cax11","architecture":"arm","locations":[{"name":"fsn1"}]}],"meta":{"pagination":{"next_page":null}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"server_types":[],"meta":{"pagination":{"next_page":2}}}`))
		case "/images":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`{"images":[{"name":"ubuntu-24.04","architecture":"arm","status":"available"}],"meta":{"pagination":{"next_page":null}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"images":[],"meta":{"pagination":{"next_page":2}}}`))
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
	if len(catalog.Locations) != 1 || catalog.Locations[0].Name != "fsn1" {
		t.Fatalf("bad locations: %+v", catalog.Locations)
	}
	if len(catalog.ServerTypes) != 1 || catalog.ServerTypes[0].Name != "cax11" || !catalog.ServerTypes[0].SupportsLocation("fsn1") {
		t.Fatalf("bad server types: %+v", catalog.ServerTypes)
	}
	if len(catalog.Images) != 1 || catalog.Images[0].Name != "ubuntu-24.04" || catalog.Images[0].Architecture != "arm" {
		t.Fatalf("bad images: %+v", catalog.Images)
	}
}
