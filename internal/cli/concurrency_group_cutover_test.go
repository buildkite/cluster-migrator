package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestConcurrencyGroupCutover(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		dryRun    bool
		json      bool
		wait      bool
		state     string
		waitError error
		wantError string
		wantPosts int
		wantWaits int
		wantText  string
	}{
		{name: "non-interactive", wantPosts: 1, wantText: "Group: deploy\nState: draining\n"},
		{name: "JSON result", json: true, wantPosts: 1, wantText: `"state": "draining"`},
		{name: "dry run", dryRun: true, wantText: "cut over concurrency group deploy in production (dry run)"},
		{name: "wait for completion", wait: true, state: "clustered", wantPosts: 1, wantWaits: 1, wantText: "Group: deploy\nState: clustered\n"},
		{name: "JSON completion", json: true, wait: true, state: "clustered", wantPosts: 1, wantWaits: 1, wantText: `"state": "clustered"`},
		{name: "failed cutover", wait: true, state: "failed", wantPosts: 1, wantWaits: 1, wantError: "concurrency-group cutover ended in failed"},
		{name: "cancelled wait", wait: true, waitError: context.Canceled, wantPosts: 1, wantWaits: 1, wantError: "context canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			posts, waits := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
					_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups/deploy":
					state := "draining"
					if waits > 0 {
						state = test.state
					}
					_, _ = fmt.Fprintf(w, `{"key":"deploy","state":%q}`, state)
				case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups/deploy/cutover":
					posts++
					_, _ = w.Write([]byte(`{"key":"deploy","state":"draining"}`))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			app := &Context{
				Context: context.Background(), Client: client, Output: &output, DryRun: test.dryRun, JSON: test.json,
				Wait: func(context.Context, time.Duration) error {
					waits++
					return test.waitError
				},
			}
			cmd := ConcurrencyGroupCutoverCmd{Group: "deploy", DestinationCluster: "production", Wait: test.wait}
			err = cmd.Run(app)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want %q", err, test.wantError)
				}
				if test.waitError != nil && !errors.Is(err, test.waitError) {
					t.Fatalf("error = %v, want %v", err, test.waitError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if posts != test.wantPosts || waits != test.wantWaits {
				t.Fatalf("posts/waits = %d/%d, want %d/%d", posts, waits, test.wantPosts, test.wantWaits)
			}
			if !strings.Contains(output.String(), test.wantText) {
				t.Fatalf("output = %q, want %q", output.String(), test.wantText)
			}
		})
	}
}
