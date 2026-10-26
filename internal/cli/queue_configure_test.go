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

func TestQueueConfigureDryRunDoesNotMutate(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/unclustered-to-clustered-migration":
			_, _ = w.Write([]byte(`{"destination_cluster_id":"cluster-id","phase":"prepare_queues"}`))
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case "/v2/organizations/acme/clusters/cluster-id/queues":
			_, _ = w.Write([]byte(`[{"id":"queue-id","key":"test"}]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"--dry-run",
		"queue", "configure", "test",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	want := "QUEUE CONFIGURE (DRY RUN)\nQueue: test\nDestination: production\n\nPROPOSED CHANGE\n\nA migration would be created with 0% routing.\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueConfigurePrintsNextStep(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/unclustered-to-clustered-migration":
			_, _ = w.Write([]byte(`{"destination_cluster_id":"cluster-id","phase":"prepare_queues"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"other-cluster","name":"development"},{"id":"cluster-id","name":"production"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters/cluster-id/queues":
			_, _ = w.Write([]byte(`[{"id":"queue-id","key":"test"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["queue_key"] != "test" || len(body) != 1 {
				t.Errorf("configuration = %v", body)
			}
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"test"},"routed_percent":0}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "configure", "test",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE CONFIGURE\nQueue: test\nDestination: production\n\nRESULT\n\nMigration created with 0% routing.\n\nNEXT STEPS\n\n1. Scale the destination infrastructure.\n\n2. Once ready, begin routing:\n\n   cluster-migrator queue set-percent test --to <percentage>\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueEditsRejectFrozenOrMissingConfiguration(t *testing.T) {
	for _, phase := range []string{"validate_queues", "cutover_concurrency_groups", "migrate_pipelines", "complete", "missing"} {
		for _, command := range [][]string{{"queue", "configure", "test"}, {"queue", "set-percent", "test", "--to", "25"}} {
			for _, flags := range [][]string{nil, {"--dry-run"}, {"--json", "--dry-run"}} {
				t.Run(phase+"/"+strings.Join(append(append([]string{}, command...), flags...), " "), func(t *testing.T) {
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					t.Setenv("BUILDKITE_API_TOKEN", "secret")
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/acme/unclustered-to-clustered-migration" {
							t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
							http.NotFound(w, r)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						if phase == "missing" {
							w.WriteHeader(http.StatusNotFound)
							_, _ = fmt.Fprint(w, `{"message":"No migration found"}`)
							return
						}
						_, _ = fmt.Fprintf(w, `{"phase":%q,"destination_cluster_id":"cluster-id"}`, phase)
					}))
					defer server.Close()
					args := append([]string{"--endpoint", server.URL}, command...)
					args = append(args, flags...)
					var stdout bytes.Buffer
					err := Run(context.Background(), args, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
					wantError := "recover forward"
					switch phase {
					case "validate_queues":
						wantError = "cluster-migrator migration prepare-queues"
					case "missing":
						wantError = "ask Buildkite to configure enrollment"
					}
					if err == nil || !strings.Contains(err.Error(), wantError) || stdout.Len() != 0 {
						t.Fatalf("error=%v stdout=%q, want %q and empty stdout", err, stdout.String(), wantError)
					}
					if phase != "validate_queues" && err != nil && strings.Contains(err.Error(), "migration prepare-queues") {
						t.Fatalf("unsafe preparation guidance: %v", err)
					}
				})
			}
		}
	}
}
