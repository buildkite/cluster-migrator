package buildkite

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolvePipelineNameRequiresUniqueExactMatch(t *testing.T) {
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
	pipeline, err := client.ResolvePipelineName(context.Background(), "Demo Pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.Slug != "demo-pipeline" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

func TestResolvePipelineNameFollowsPagination(t *testing.T) {
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
	pipeline, err := client.ResolvePipelineName(context.Background(), "Demo Pipeline")
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.Slug != "demo-pipeline" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

func TestResolvePipelineNameRejectsAmbiguousName(t *testing.T) {
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
	_, err = client.ResolvePipelineName(context.Background(), "Demo Pipeline")
	if err == nil || err.Error() != `multiple pipelines match name "Demo Pipeline"; use a slug: demo-one, demo-two` {
		t.Fatalf("error = %q", err)
	}
}

func TestResolvePipelineNameReportsMissingNameOrSlug(t *testing.T) {
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
	_, err = client.ResolvePipelineName(context.Background(), "Missing Pipeline")
	if err == nil || !strings.Contains(err.Error(), `no pipeline found matching name or slug "Missing Pipeline"`) {
		t.Fatalf("error = %q", err)
	}
}
