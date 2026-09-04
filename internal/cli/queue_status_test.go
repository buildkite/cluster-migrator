package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueueStatusPrintsTable(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"Cluster Migrator Demo"}]`))
		case "/v2/organizations/acme/cluster-queue-migrations/default":
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
	want := "QUEUE STATUS\nQueue: default\nDestination: Cluster Migrator Demo\n\nRESULT\n\nRouting: 25%\n"
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
		case "/v2/organizations/acme/cluster-queue-migrations/default":
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
	want := "QUEUE STATUS\nQueue: default\nDestination: Cluster Migrator Demo\n\nRESULT\n\nRouting: 0%\n\nNEXT\n\nScale the destination infrastructure. When ready, begin routing:\n\n  cluster-migrator queue set-percent default --to <percentage>\n"
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
		case "/v2/organizations/acme/cluster-queue-migrations":
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
			want: "QUEUE STATUS\nMigrations: 0\n\nRESULT\n\nNo queue migrations configured.\n\nNEXT\n\nConfigure a queue migration:\n\n  cluster-migrator queue configure <queue> --destination-cluster <cluster>\n",
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
		case "/v2/organizations/acme/cluster-queue-migrations":
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
	want := "QUEUE STATUS\nMigrations: 2\n\nRESULT\n\nQUEUE   ROUTING  DESTINATION\ndeploy  75%      Production\ntest    10%      Development\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
