package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestConcurrencyGroupStatusProcess(t *testing.T) {
	const collection = "/v2/organizations/acme/concurrency-group-migrations"
	const migration = `{"scope":"custom","key":"deploy","state":"forward_draining","destination_cluster_id":"cluster-id","held_at":null,"source_jobs":{"running":1,"waiting":0},"url":""}`
	for _, test := range []struct {
		name         string
		args         []string
		wantStdout   string
		wantJSON     bool
		wantError    string
		wantRequests []string
	}{
		{name: "show", args: []string{"custom/deploy"}, wantStdout: "Group: custom/deploy\nDestination: production\n", wantRequests: []string{collection + "/custom/deploy", "/v2/organizations/acme/clusters"}},
		{name: "JSON show", args: []string{"custom/deploy", "--json"}, wantJSON: true, wantRequests: []string{collection + "/custom/deploy"}},
		{name: "empty list", wantStdout: "No concurrency-group migrations found.\n", wantRequests: []string{collection}},
		{name: "JSON empty list", args: []string{"--json"}, wantStdout: "[]\n", wantJSON: true, wantRequests: []string{collection}},
		{name: "no migration", args: []string{"step/123"}, wantError: "no migration has started for concurrency group step/123", wantRequests: []string{collection + "/step/123"}},
		{name: "unscoped group", args: []string{"deploy"}, wantError: `concurrency group "deploy" must be <scope>/<key>, where scope is custom or step`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v2/organizations" {
					_, _ = fmt.Fprint(w, `[{"slug":"acme"}]`)
					return
				}
				requests = append(requests, r.URL.Path)
				switch r.URL.Path {
				case collection:
					_, _ = fmt.Fprint(w, `{"items":[],"links":{}}`)
				case collection + "/custom/deploy":
					_, _ = fmt.Fprint(w, migration)
				case "/v2/organizations/acme/clusters":
					_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"production"}]`)
				default:
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"message":"No concurrency group migration found"}`)
				}
			}))
			defer server.Close()

			args := append([]string{"--endpoint", server.URL, "concurrency-group", "status"}, test.args...)
			stdout, stderr, exitCode := runCLI(t, args...)

			if test.wantError != "" {
				if exitCode != 1 || stdout != "" || stderr != "cluster-migrator: "+test.wantError+"\n" {
					t.Fatalf("exit=%d stdout=%q stderr=%q; want exit=1, empty stdout, stderr %q", exitCode, stdout, stderr, test.wantError)
				}
			} else {
				if exitCode != 0 || stderr != "" {
					t.Fatalf("exit=%d stderr=%q; want exit=0 and empty stderr", exitCode, stderr)
				}
				if test.wantJSON && !json.Valid([]byte(stdout)) {
					t.Fatalf("invalid JSON stdout: %q", stdout)
				}
				if !strings.Contains(stdout, test.wantStdout) {
					t.Fatalf("stdout = %q, want %q", stdout, test.wantStdout)
				}
			}
			if strings.Join(requests, ",") != strings.Join(test.wantRequests, ",") {
				t.Fatalf("requests = %v, want %v", requests, test.wantRequests)
			}
		})
	}
}

func TestConcurrencyGroupCutoverProcess(t *testing.T) {
	const groupPath = "/v2/organizations/acme/concurrency-group-migrations/custom/deploy"
	for _, test := range []struct {
		name      string
		args      []string
		ready     bool
		wantPosts int
		wantJSON  bool
		wantText  string
		wantError string
	}{
		{name: "dry run", args: []string{"--dry-run"}, ready: true, wantText: "A cutover to the production cluster would be requested.\n"},
		{name: "JSON dry run", args: []string{"--dry-run", "--json"}, ready: true, wantJSON: true},
		{name: "started", ready: true, wantPosts: 1, wantText: "Cutover to the production cluster started.\n"},
		{name: "blocked", wantPosts: 1, wantText: "Cutover rejected without changes. Blocked by 1 known condition.\n", wantError: "concurrency group has known cutover blockers"},
		{name: "JSON blocked", args: []string{"--json"}, wantPosts: 1, wantJSON: true, wantError: "concurrency group has known cutover blockers"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations":
					_, _ = fmt.Fprint(w, `[{"slug":"acme"}]`)
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
					_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"production"}]`)
				case r.Method == http.MethodGet && r.URL.Path == groupPath:
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"message":"No concurrency group migration found"}`)
				case r.Method == http.MethodPost && r.URL.Path == groupPath+"/cutover":
					posts++
					if !test.ready {
						w.WriteHeader(http.StatusConflict)
						_, _ = fmt.Fprint(w, `{"message":"Concurrency group is not ready to cut over","ready":false,"status":"blocked","migration":{"state":"unclustered","blocking_reason":null},"queue_observation":{"window_seconds":600,"queues":["deploy"],"blocking_queues":[{"queue":"deploy","reasons":["recent_source_jobs"],"routed_percent":100,"recent_source_jobs":1}]}}`)
						return
					}
					w.WriteHeader(http.StatusAccepted)
					_, _ = fmt.Fprint(w, `{"scope":"custom","key":"deploy","state":"forward_draining","destination_cluster_id":"cluster-id"}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			args := append([]string{"--endpoint", server.URL, "concurrency-group", "cutover", "custom/deploy", "--destination-cluster", "production"}, test.args...)
			stdout, stderr, exitCode := runCLI(t, args...)
			wantExit, wantStderr := 0, ""
			if test.wantError != "" {
				wantExit, wantStderr = 1, "cluster-migrator: "+test.wantError+"\n"
			}
			if exitCode != wantExit || stderr != wantStderr || posts != test.wantPosts {
				t.Fatalf("exit=%d stderr=%q posts=%d; want exit=%d stderr=%q posts=%d", exitCode, stderr, posts, wantExit, wantStderr, test.wantPosts)
			}
			if test.wantJSON && !json.Valid([]byte(stdout)) {
				t.Fatalf("invalid JSON stdout: %q", stdout)
			}
			if !strings.Contains(stdout, test.wantText) {
				t.Fatalf("stdout = %q, want %q", stdout, test.wantText)
			}
		})
	}
}

func TestConcurrencyGroupRejectsUnscopedGroupsBeforeMigrationRequests(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/organizations" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"slug":"acme"}]`)
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, "--endpoint", server.URL, "concurrency-group", "cutover", "deploy", "--destination-cluster", "production")
	want := "cluster-migrator: concurrency group \"deploy\" must be <scope>/<key>, where scope is custom or step\n"
	if exitCode != 1 || stdout != "" || stderr != want {
		t.Fatalf("exit=%d stdout=%q stderr=%q; want exit=1, empty stdout, stderr=%q", exitCode, stdout, stderr, want)
	}
}

func TestParseErrorsPrintContextualUsage(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		errorText string
		usage     string
		includes  string
		excludes  string
	}{
		{
			name:      "unknown root command",
			args:      []string{"unknown"},
			errorText: "cluster-migrator: unexpected argument unknown",
			usage:     "Usage: cluster-migrator <command> [flags]",
			includes:  "pipeline readiness",
			excludes:  "Usage: cluster-migrator queue ",
		},
		{
			name:      "unknown queue command",
			args:      []string{"queue", "set-percentage", "to==10", "blue"},
			errorText: "cluster-migrator: unexpected argument set-percentage",
			usage:     "Usage: cluster-migrator queue <command> [flags]",
			includes:  "queue set-percent",
			excludes:  "pipeline readiness",
		},
		{
			name:      "malformed command argument",
			args:      []string{"queue", "set-percent", "blue", "to==10"},
			errorText: "cluster-migrator: unexpected argument to==10",
			usage:     "Usage: cluster-migrator queue set-percent --to=INT <queue>",
			excludes:  "queue configure",
		},
		{
			name:      "invalid command flag",
			args:      []string{"queue", "set-percent", "blue", "--to=ten"},
			errorText: "cluster-migrator: --to: expected a valid 64 bit int but got \"ten\"",
			usage:     "Usage: cluster-migrator queue set-percent --to=INT <queue>",
			excludes:  "queue configure",
		},
		{
			name:      "missing command flag",
			args:      []string{"queue", "set-percent", "blue"},
			errorText: "cluster-migrator: missing flags: --to=INT",
			usage:     "Usage: cluster-migrator queue set-percent --to=INT <queue>",
			excludes:  "queue configure",
		},
		{
			name:      "missing positional argument",
			args:      []string{"queue", "set-percent", "--to=10"},
			errorText: "cluster-migrator: expected \"<queue>\"",
			usage:     "Usage: cluster-migrator queue set-percent --to=INT <queue>",
			excludes:  "queue configure",
		},
		{
			name:      "unknown root flag",
			args:      []string{"--unknown"},
			errorText: "cluster-migrator: unknown flag --unknown",
			usage:     "Usage: cluster-migrator <command> [flags]",
			excludes:  "Usage: cluster-migrator queue ",
		},
		{
			name:      "removed pipeline move wait flag",
			args:      []string{"pipeline", "move", "monorepo", "--destination-cluster=production", "--wait"},
			errorText: "cluster-migrator: unknown flag --wait",
			usage:     "Usage: cluster-migrator pipeline move --destination-cluster=STRING <pipeline>",
			excludes:  "Wait until the pipeline reports",
		},
		{
			name:      "removed queue destination override",
			args:      []string{"queue", "configure", "test", "--destination-cluster=production"},
			errorText: "cluster-migrator: unknown flag --destination-cluster",
			usage:     "Usage: cluster-migrator queue configure <queue>",
			excludes:  "Destination cluster name or ID.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(t, test.args...)
			if exitCode == 0 {
				t.Fatal("exit code = 0, want non-zero")
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, test.usage) {
				t.Errorf("stderr does not contain contextual usage %q:\n%s", test.usage, stderr)
			}
			if test.includes != "" && !strings.Contains(stderr, test.includes) {
				t.Errorf("stderr does not contain %q:\n%s", test.includes, stderr)
			}
			if test.excludes != "" && strings.Contains(stderr, test.excludes) {
				t.Errorf("stderr unexpectedly contains %q:\n%s", test.excludes, stderr)
			}
			if count := strings.Count(stderr, test.errorText); count != 1 {
				t.Errorf("error count = %d, want 1 for %q:\n%s", count, test.errorText, stderr)
			}
		})
	}
}

func TestRuntimeErrorDoesNotPrintUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, exitCode := runCLI(t,
		"--endpoint="+server.URL,
		"queue", "status",
	)
	if exitCode == 0 {
		t.Fatal("exit code = 0, want non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatalf("stderr unexpectedly contains usage:\n%s", stderr)
	}
	if count := strings.Count(stderr, "cluster-migrator:"); count != 1 {
		t.Fatalf("error count = %d, want 1:\n%s", count, stderr)
	}
}

func TestHelpAndVersionRemainOnStdout(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		contains string
	}{
		{name: "root help", args: []string{"--help"}, contains: "Usage: cluster-migrator "},
		{name: "queue help", args: []string{"queue", "--help"}, contains: "Usage: cluster-migrator queue "},
		{name: "version", args: []string{"--version"}, contains: "cluster-migrator dev\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(t, test.args...)
			if exitCode != 0 {
				t.Fatalf("exit code = %d, want 0; stderr:\n%s", exitCode, stderr)
			}
			if !strings.Contains(stdout, test.contains) {
				t.Fatalf("stdout does not contain %q:\n%s", test.contains, stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func runCLI(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	command := exec.Command(os.Args[0], append([]string{"-test.run=TestCLIProcess", "--"}, args...)...)
	command.Env = append(os.Environ(),
		"CLUSTER_MIGRATOR_TEST_PROCESS=1",
		"BUILDKITE_API_TOKEN=test",
	)
	var stdoutBuffer, stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	err := command.Run()
	if err == nil {
		return stdoutBuffer.String(), stderrBuffer.String(), 0
	}
	exitError, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("run CLI: %v", err)
	}
	return stdoutBuffer.String(), stderrBuffer.String(), exitError.ExitCode()
}

func TestCLIProcess(_ *testing.T) {
	if os.Getenv("CLUSTER_MIGRATOR_TEST_PROCESS") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"cluster-migrator"}, os.Args[index+1:]...)
			main()
			os.Exit(0) // Do not append the test runner's PASS line to CLI stdout.
		}
	}
	panic("missing argument separator")
}
