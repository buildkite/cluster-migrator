package buildkite

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveClusterFollowsLinkHeader(t *testing.T) {
	t.Parallel()

	server := paginatedLookupServer(t, "/v2/organizations/acme/clusters", `[{"id":"cluster-id","name":"production"}]`)
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	cluster, err := client.ResolveCluster(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	if cluster.ID != "cluster-id" {
		t.Fatalf("cluster = %#v", cluster)
	}
}

func TestRequireClusterQueueFollowsLinkHeader(t *testing.T) {
	t.Parallel()

	path := "/v2/organizations/acme/clusters/cluster-id/queues"
	server := paginatedLookupServer(t, path, `[{"id":"queue-id","key":"test"}]`)
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RequireClusterQueue(context.Background(), "cluster-id", "test"); err != nil {
		t.Fatal(err)
	}
}

func paginatedLookupServer(t *testing.T, path, secondPage string) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Fatalf("path = %q, want %q", r.URL.Path, path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(secondPage))
			return
		}
		w.Header().Set("Link", fmt.Sprintf("<%s%s?page=2>; rel=%cnext%c", server.URL, path, '"', '"'))
		_, _ = w.Write([]byte(`[]`))
	}))
	return server
}
