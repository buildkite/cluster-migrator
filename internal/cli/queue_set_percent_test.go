package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueueSetPercentValidatesRange(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "101",
	}, &bytes.Buffer{}, &bytes.Buffer{}, organizationClient(server.Client()))
	if err == nil || !strings.Contains(err.Error(), "between 0 and 100") {
		t.Fatalf("error = %v", err)
	}
}

func TestQueueSetPercentPrintsChange(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/unclustered-to-clustered-migration":
			_, _ = w.Write([]byte(`{"destination_cluster_id":"cluster-id","phase":"prepare_queues"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":25}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "25",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE SET-PERCENT\nQueue: test\nDestination: production\n\nRESULT\n\nRouting changed from 10% to 25%.\n\nNEXT STEPS\n\n1. Review destination activity:\n\n   cluster-migrator queue metrics test\n\n2. When ready, increase routing:\n\n   cluster-migrator queue set-percent test --to <percentage>\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueSetPercentToZeroPrintsBeginRoutingGuidance(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/unclustered-to-clustered-migration":
			_, _ = w.Write([]byte(`{"destination_cluster_id":"cluster-id","phase":"prepare_queues"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":25}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":0}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "0",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE SET-PERCENT\nQueue: test\nDestination: production\n\nRESULT\n\nRouting changed from 25% to 0%.\n\nNEXT STEPS\n\n1. Scale the destination infrastructure.\n\n2. Once ready, begin routing:\n\n   cluster-migrator queue set-percent test --to <percentage>\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueSetPercentToHundredPrintsValidationGuidance(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/unclustered-to-clustered-migration":
			_, _ = w.Write([]byte(`{"destination_cluster_id":"cluster-id","phase":"prepare_queues"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":75}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"Cluster Migrator Demo"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":100}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "100",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE SET-PERCENT\nQueue: test\nDestination: Cluster Migrator Demo\n\nRESULT\n\nRouting changed from 75% to 100%.\n\nNEXT STEPS\n\n1. Once all queue mappings are prepared, test representative workloads during validation:\n\n   cluster-migrator migration set-phase validate-queues\n\n2. Inspect the organization migration before further operations:\n\n   cluster-migrator migration status\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueSetPercentCanDecreaseFromHundred(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			patches := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/unclustered-to-clustered-migration"):
					_, _ = fmt.Fprint(w, `{"phase":"prepare_queues","destination_cluster_id":"cluster-id"}`)
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/clusters"):
					_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"production"}]`)
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cluster-queue-migrations/test"):
					_, _ = fmt.Fprint(w, `{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":100}`)
				case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/cluster-queue-migrations/test"):
					patches++
					var body struct {
						Percent int `json:"routed_percent"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Percent != 25 {
						t.Errorf("body=%+v error=%v", body, err)
					}
					_, _ = fmt.Fprint(w, `{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":25}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			args := []string{"--endpoint", server.URL, "--json", "queue", "set-percent", "test", "--to", "25"}
			if dryRun {
				args = append(args, "--dry-run")
			}
			var stdout bytes.Buffer
			if err := Run(context.Background(), args, &stdout, &bytes.Buffer{}, organizationClient(server.Client())); err != nil {
				t.Fatal(err)
			}
			var result QueueChange
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			wantPatches := 1
			if dryRun {
				wantPatches = 0
			}
			if result.FromPercent == nil || *result.FromPercent != 100 || result.ToPercent != 25 || result.DryRun != dryRun || patches != wantPatches {
				t.Fatalf("result=%+v patches=%d", result, patches)
			}
		})
	}
}
