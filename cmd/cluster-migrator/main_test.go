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

func TestConcurrencyGroupProcess(t *testing.T) {
	for _, test := range []struct {
		name      string
		args      []string
		state     string
		blocked   bool
		wantPosts int
		wantText  string
		wantError string
	}{
		{name: "status", args: []string{"status", "deploy"}, state: "draining", wantText: "CONCURRENCY-GROUP STATUS\nGroup: deploy\n"},
		{name: "empty list", args: []string{"status"}, wantText: "No concurrency groups found.\n"},
		{name: "accepted", args: []string{"cutover", "deploy", "--destination-cluster", "production"}, state: "draining", wantPosts: 1, wantText: "Cutover request accepted.\n"},
		{name: "dry run", args: []string{"cutover", "deploy", "--destination-cluster", "production", "--dry-run"}, state: "unclustered", wantText: "CONCURRENCY-GROUP CUTOVER (DRY RUN)\n"},
		{name: "blocked dry run", args: []string{"cutover", "deploy", "--destination-cluster", "production", "--dry-run"}, state: "blocked", blocked: true, wantText: "Cutover is blocked by queues.\n", wantError: "concurrency group is blocked by queues: [test release]"},
		{name: "completed", args: []string{"cutover", "deploy", "--destination-cluster", "production", "--wait"}, state: "clustered", wantPosts: 1, wantText: "Cutover completed.\n"},
		{name: "failed", args: []string{"cutover", "deploy", "--destination-cluster", "production", "--wait"}, state: "failed", wantPosts: 1, wantError: "concurrency-group cutover ended in failed"},
		{name: "cancelled", args: []string{"cutover", "deploy", "--destination-cluster", "production", "--wait"}, state: "cancelled", wantPosts: 1, wantError: "concurrency-group cutover ended in cancelled"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", test.name, jsonOutput), func(t *testing.T) {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				posts := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					const groupPath = "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups/deploy"
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations":
						_, _ = fmt.Fprint(w, `[{"slug":"acme"}]`)
					case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
						_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"production"}]`)
					case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups":
						_, _ = fmt.Fprint(w, `{"items":[],"links":{}}`)
					case r.Method == http.MethodGet && r.URL.Path == groupPath,
						r.Method == http.MethodPost && r.URL.Path == groupPath+"/cutover":
						if r.Method == http.MethodPost {
							posts++
						}
						queues := `[]`
						if test.blocked {
							queues = `["test","release"]`
						}
						_, _ = fmt.Fprintf(w, `{"key":"deploy","state":%q,"destination_cluster_id":"cluster-id","blocking_queues":%s,"running_source_jobs":0,"waiting_jobs":0,"url":""}`, test.state, queues)
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer server.Close()
				args := append([]string{"--endpoint", server.URL, "concurrency-group"}, test.args...)
				if jsonOutput {
					args = append(args, "--json")
				}
				stdout, stderr, exitCode := runCLI(t, args...)
				wantExit, wantStderr := 0, ""
				if test.wantError != "" {
					wantExit, wantStderr = 1, "cluster-migrator: "+test.wantError+"\n"
				}
				if exitCode != wantExit || stderr != wantStderr || posts != test.wantPosts {
					t.Fatalf("exit=%d stderr=%q posts=%d; want exit=%d stderr=%q posts=%d", exitCode, stderr, posts, wantExit, wantStderr, test.wantPosts)
				}
				if jsonOutput && test.wantError == "" {
					if !json.Valid([]byte(stdout)) {
						t.Fatalf("invalid JSON: %s", stdout)
					}
				} else if (jsonOutput || test.wantText == "") && stdout != "" {
					t.Fatalf("unexpected stdout: %q", stdout)
				} else if !jsonOutput && !strings.Contains(stdout, test.wantText) {
					t.Fatalf("stdout = %q, want %q", stdout, test.wantText)
				}
				t.Logf("exit=%d\nstdout:\n%sstderr:\n%s", exitCode, stdout, stderr)
			})
		}
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
