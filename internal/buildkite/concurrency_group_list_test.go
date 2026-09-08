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
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups" {
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
					_, _ = w.Write([]byte(`{"items":[{"key":"second"}],"links":{}}`))
					return
				}
				_, _ = fmt.Fprintf(w, `{"items":[{"key":"first"}],"links":{"next":%q}}`, server.URL+"/v2/organizations/acme/cluster-queue-migrations/concurrency-groups?after=first")
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
			if len(groups) != 2 || groups[0].Key != "first" || groups[1].Key != "second" {
				t.Fatalf("groups = %#v", groups)
			}
		})
	}
}
