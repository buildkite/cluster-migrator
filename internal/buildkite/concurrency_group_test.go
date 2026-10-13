package buildkite

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListConcurrencyGroupsFollowsNextLinks(t *testing.T) {
	t.Parallel()

	for _, failNextPage := range []bool{false, true} {
		t.Run(fmt.Sprintf("failNextPage=%t", failNextPage), func(t *testing.T) {
			t.Parallel()
			const path = "/v2/organizations/acme/concurrency-group-migrations"
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("after") == "first" {
					if failNextPage {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					_, _ = w.Write([]byte(`{"items":[{"scope":"step","key":"deploy"}],"links":{}}`))
					return
				}
				if got := r.URL.Query().Get("per_page"); got != "100" {
					t.Errorf("per_page = %q, want 100", got)
				}
				_, _ = fmt.Fprintf(w, `{"items":[{"scope":"custom","key":"deploy"}],"links":{"next":%q}}`, server.URL+path+"?after=first")
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			groups, err := client.ListConcurrencyGroups(context.Background())
			if failNextPage {
				if err == nil || groups != nil {
					t.Fatalf("groups = %#v, error = %v; want no partial result and an error", groups, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(groups) != 2 || groups[0].Scope != "custom" || groups[1].Scope != "step" {
				t.Fatalf("groups = %#v", groups)
			}
		})
	}
}

func TestGetConcurrencyGroupSendsScopeAndEscapedKey(t *testing.T) {
	t.Parallel()

	const want = "/v2/organizations/acme/concurrency-group-migrations/custom/deploy%2Fproduction"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != want {
			t.Errorf("request = %s %s, want GET %s", r.Method, r.URL.EscapedPath(), want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scope":"custom","key":"deploy/production","state":"forward_draining","destination_cluster_id":"cluster-id","held_at":"2026-10-09T07:00:00.000Z","source_jobs":{"running":2,"waiting":3},"url":""}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	group, err := client.GetConcurrencyGroup(context.Background(), "custom", "deploy/production")
	if err != nil {
		t.Fatal(err)
	}
	if group.State != ConcurrencyGroupForwardDraining || group.SourceJobs == nil || group.SourceJobs.Waiting != 3 || group.DestinationClusterID != "cluster-id" {
		t.Fatalf("group = %#v", group)
	}
}
