package buildkite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rollbackResponse = `{"pipeline":"monorepo","cluster_id":null,"assignment_changed":true,"cutoff":"2026-09-08T01:00:00Z","scanned_through":"2026-09-08T03:00:01Z","passes":2,"selected":2,"cancellation_enqueued":1,"pending":2,"failures":[{"build_uuid":"build-uuid","message":"Permission denied"}],"best_effort":true}`

func TestRollbackPipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/v2/organizations/acme/cluster-queue-migrations/pipelines/mono%2Frepo/rollback" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if string(body) != "{}" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request body or headers: body=%q", body)
		}
		_, _ = fmt.Fprint(w, strings.ReplaceAll(rollbackResponse, `Z"`, `.123456Z"`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.RollbackPipeline(context.Background(), "mono/repo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Pipeline != "monorepo" || !result.AssignmentChanged || result.ClusterID != nil || result.CancellationEnqueued != 1 || result.Pending != 2 || len(result.Failures) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Cutoff != "2026-09-08T01:00:00.123456Z" || result.ScannedThrough != "2026-09-08T03:00:01.123456Z" {
		t.Fatalf("timestamp precision lost: %#v", result)
	}
}

func TestRollbackPipelineRejectsInvalidResponse(t *testing.T) {
	tests := map[string]string{
		"empty":              `{}`,
		"null":               `null`,
		"still assigned":     strings.Replace(rollbackResponse, `"cluster_id":null`, `"cluster_id":"cluster-id"`, 1),
		"missing cluster":    strings.Replace(rollbackResponse, `"cluster_id":null,`, "", 1),
		"invalid cutoff":     strings.Replace(rollbackResponse, "2026-09-08T01:00:00Z", "invalid", 1),
		"invalid scan time":  strings.Replace(rollbackResponse, "2026-09-08T03:00:01Z", "invalid", 1),
		"scan before cutoff": strings.Replace(rollbackResponse, "2026-09-08T03:00:01Z", "2026-09-08T00:00:00Z", 1),
		"negative pending":   strings.Replace(rollbackResponse, `"pending":2`, `"pending":-1`, 1),
		"excess enqueues":    strings.Replace(rollbackResponse, `"cancellation_enqueued":1`, `"cancellation_enqueued":3`, 1),
		"empty pipeline":     strings.Replace(rollbackResponse, `"pipeline":"monorepo"`, `"pipeline":""`, 1),
		"wrong passes":       strings.Replace(rollbackResponse, `"passes":2`, `"passes":0`, 1),
		"not best effort":    strings.Replace(rollbackResponse, `"best_effort":true`, `"best_effort":false`, 1),
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rollbackResponse), &fields); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if key == "cluster_id" {
			continue
		}
		delete(fields, key)
		missing, _ := json.Marshal(fields)
		tests["missing "+key] = string(missing)
		fields[key] = json.RawMessage(`null`)
		null, _ := json.Marshal(fields)
		tests["null "+key] = string(null)
		fields[key] = value
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, response)
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.RollbackPipeline(context.Background(), "monorepo"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestRollbackPipelineAPIErrors(t *testing.T) {
	for _, status := range []int{400, 403, 404, 422, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"message":"Rollback unavailable","code":"rollback_unavailable"}`)
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.RollbackPipeline(context.Background(), "monorepo")
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != status || apiError.Code != "rollback_unavailable" {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
