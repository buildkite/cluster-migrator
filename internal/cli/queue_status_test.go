package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrationInventoryConfigurationNotice(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	for _, command := range []string{"queue", "concurrency-group"} {
		for _, header := range []string{"false", "true", ""} {
			for _, jsonOutput := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/header=%s/json=%t", command, header, jsonOutput), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						switch r.URL.Path {
						case "/v2/organizations/acme/clusters":
							_, _ = w.Write([]byte(`[]`))
						case "/v2/organizations/acme/unclustered-to-clustered/queue-migrations", "/v2/organizations/acme/unclustered-to-clustered/concurrency-group-migrations":
							if header != "" {
								w.Header().Set("Buildkite-Migration-Configured", header)
							}
							_, _ = w.Write([]byte(`{"items":[],"links":{}}`))
						default:
							t.Errorf("unexpected request: %s", r.URL.Path)
							w.WriteHeader(http.StatusNotFound)
						}
					}))
					defer server.Close()
					args := []string{"--endpoint", server.URL, command, "status"}
					if jsonOutput {
						args = append(args, "--json")
					}
					var stdout, stderr bytes.Buffer
					if err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client())); err != nil {
						t.Fatal(err)
					}
					if jsonOutput && strings.TrimSpace(stdout.String()) != "[]" {
						t.Fatalf("inventory stdout = %q", stdout.String())
					}
					if header == "false" {
						if !strings.Contains(stderr.String(), `cluster-migrator migration configure --destination-cluster="<cluster name or ID>"`) || strings.Contains(stdout.String(), "migration configure") {
							t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
						}
					} else if stderr.Len() != 0 {
						t.Fatalf("unexpected notice = %q", stderr.String())
					}
				})
			}
		}
	}
}

func TestQueueStatusPrintsTable(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"Cluster Migrator Demo"}]`))
		case "/v2/organizations/acme/unclustered-to-clustered/queue-migrations/default":
			_, _ = w.Write([]byte(`{"queue_key":"default","destination":{"cluster_id":"cluster-id"},"routed_percent":25}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "status", "default",
	}, &stdout, &stderr, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE STATUS\nMigrations: 1\n\nSTATUS\n\nQUEUE    ROUTING  DESTINATION\ndefault  25%      Cluster Migrator Demo\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if got, want := stderr.String(), ""; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestQueueStatusAtZeroPrintsNextStep(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"Cluster Migrator Demo"}]`))
		case "/v2/organizations/acme/unclustered-to-clustered/queue-migrations/default":
			_, _ = w.Write([]byte(`{"queue_key":"default","destination":{"cluster_id":"cluster-id"},"routed_percent":0}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "status", "default",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE STATUS\nMigrations: 1\n\nSTATUS\n\nQUEUE    ROUTING  DESTINATION\ndefault  0%       Cluster Migrator Demo\n\nNEXT STEPS\n\n1. Inspect the organization migration's phase before changing configuration or starting further operations:\n\n   cluster-migrator migration status\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueStatusEmptyListOutput(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[]`))
		case "/v2/organizations/acme/unclustered-to-clustered/queue-migrations":
			_, _ = w.Write([]byte(`{"items":[],"links":{}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "human-readable",
			args: []string{"--endpoint", server.URL, "queue", "status"},
			want: "QUEUE STATUS\nMigrations: 0\n\nSTATUS\n\nNo queue migrations are configured.\n",
		},
		{
			name: "JSON",
			args: []string{"--endpoint", server.URL, "--json", "queue", "status"},
			want: "[]\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := Run(context.Background(), test.args, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			if got := stdout.String(); got != test.want {
				t.Fatalf("output = %q, want %q", got, test.want)
			}
		})
	}
}

func TestQueueStatusPrintsList(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-a","name":"Production"},{"id":"cluster-b","name":"Development"}]`))
		case "/v2/organizations/acme/unclustered-to-clustered/queue-migrations":
			_, _ = w.Write([]byte(`{"items":[{"queue_key":"deploy","destination":{"cluster_id":"cluster-a"},"routed_percent":75},{"queue_key":"test","destination":{"cluster_id":"cluster-b"},"routed_percent":10}],"links":{}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "status",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE STATUS\nMigrations: 2\n\nSTATUS\n\nQUEUE   ROUTING  DESTINATION\ndeploy  75%      Production\ntest    10%      Development\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueStatusAtHundredPrintsMigrationGuidance(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"Production"}]`))
		case "/v2/organizations/acme/unclustered-to-clustered/queue-migrations/default":
			_, _ = w.Write([]byte(`{"queue_key":"default","destination":{"cluster_id":"cluster-id"},"routed_percent":100}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "status", "default",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE STATUS\nMigrations: 1\n\nSTATUS\n\nQUEUE    ROUTING  DESTINATION\ndefault  100%     Production\n\nNEXT STEPS\n\n1. Inspect the organization migration's phase before changing configuration or starting further operations:\n\n   cluster-migrator migration status\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
