package buildkite

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolvePipelineIdentifierRequiresUniqueExactNameMatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/organizations/acme/pipelines" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("name"); got != "Demo Pipeline" {
			t.Fatalf("name = %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("per_page = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"Demo Pipeline Extra","slug":"demo-pipeline-extra"},
			{"name":"demo pipeline","slug":"lowercase-demo-pipeline"},
			{"name":"Demo Pipeline","slug":"demo-pipeline"}
		]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := client.ResolvePipelineIdentifier(context.Background(), "Demo Pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.Slug != "demo-pipeline" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

func TestResolvePipelineIdentifierFollowsPagination(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[{"name":"Demo Pipeline","slug":"demo-pipeline"}]`))
			return
		}
		w.Header().Set("Link", fmt.Sprintf("<%s/v2/organizations/acme/pipelines?name=Demo+Pipeline&page=2&per_page=100>; rel=%cnext%c", server.URL, '"', '"'))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := client.ResolvePipelineIdentifier(context.Background(), "Demo Pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.Slug != "demo-pipeline" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

func TestResolvePipelineIdentifierMatchesIDWithoutNameFilter(t *testing.T) {
	t.Parallel()

	const pipelineID = "849411f9-9e6d-4739-a0d8-e247088e9b52"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "" {
			t.Fatalf("name = %q, want no filter", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"11111111-1111-1111-1111-111111111111","slug":"other"},
			{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","slug":"demo-pipeline"}
		]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := client.ResolvePipelineIdentifier(context.Background(), pipelineID)
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.Slug != "demo-pipeline" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

func TestResolvePipelineIdentifierRejectsAmbiguousName(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"Demo Pipeline","slug":"demo-two"},
			{"name":"Demo Pipeline","slug":"demo-one"}
		]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ResolvePipelineIdentifier(context.Background(), "Demo Pipeline")
	if err == nil || err.Error() != `multiple pipelines match name "Demo Pipeline"; use an ID or slug: demo-one, demo-two` {
		t.Fatalf("error = %q", err)
	}
}

func TestResolvePipelineIdentifierReportsMissingIdentifier(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ResolvePipelineIdentifier(context.Background(), "Missing Pipeline")
	if err == nil || !strings.Contains(err.Error(), `no pipeline found matching ID, name, or slug "Missing Pipeline"`) {
		t.Fatalf("error = %q", err)
	}
}
