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

func TestPipelineRollbackPartialResultProcess(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%t", jsonOutput), func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test" {
					t.Error("missing authorization")
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations":
					_, _ = fmt.Fprint(w, `[{"slug":"acme"}]`)
				case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/rollback":
					_, _ = fmt.Fprint(w, `{"pipeline":"monorepo","cluster_id":null,"assignment_changed":true,"cutoff":"2026-09-08T01:00:00Z","scanned_through":"2026-09-08T03:00:01Z","passes":2,"selected":2,"cancellation_enqueued":1,"pending":2,"failures":[{"build_uuid":"build-uuid","message":"Enqueue failed"}],"best_effort":true}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			args := []string{"--endpoint", server.URL, "pipeline", "rollback", "monorepo"}
			if jsonOutput {
				args = append(args, "--json")
			}
			stdout, stderr, exitCode := runCLI(t, args...)
			if exitCode != 1 || !strings.Contains(stderr, "cleanup incomplete: 2 pending, 1 enqueue failures") || strings.Contains(stderr, "Usage:") {
				t.Fatalf("exit=%d stderr=%s", exitCode, stderr)
			}
			if jsonOutput {
				if !json.Valid([]byte(stdout)) || !strings.Contains(stdout, `"cluster_id": null`) || !strings.Contains(stdout, `"pending": 2`) {
					t.Fatalf("invalid or incomplete JSON: %s", stdout)
				}
			} else if !strings.Contains(stdout, "Cancellation enqueued: 1") || !strings.Contains(stdout, "build-uuid  Enqueue failed") {
				t.Fatalf("incomplete result: %s", stdout)
			}
			t.Logf("exit=%d\nstdout:\n%sstderr:\n%s", exitCode, stdout, stderr)
		})
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
			return
		}
	}
	panic("missing argument separator")
}
