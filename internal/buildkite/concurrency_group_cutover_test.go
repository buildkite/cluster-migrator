package buildkite

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartConcurrencyGroupCutover(t *testing.T) {
	t.Parallel()

	const groupPath = "/v2/organizations/acme/unclustered-to-clustered/concurrency-group-migrations/custom/deploy%2Fproduction/cutover"
	for _, test := range []struct {
		name        string
		status      int
		body        string
		wantState   string
		wantBlocked bool
		wantAPI     bool
	}{
		{name: "accepted", status: http.StatusAccepted, body: `{"scope":"custom","key":"deploy/production","state":"forward_draining"}`, wantState: ConcurrencyGroupForwardDraining},
		{
			name: "blocked", status: http.StatusConflict, wantBlocked: true,
			body: `{"message":"Concurrency group is not ready to cut over","ready":false,"status":"blocked","migration":{"state":"unclustered","blocking_reason":null},"queue_observation":{"window_seconds":600,"queues":["deploy"],"blocking_queues":[{"queue":"deploy","reasons":["recent_source_jobs"],"routed_percent":100,"recent_source_jobs":2}]}}`,
		},
		{name: "concurrent start", status: http.StatusConflict, body: `{"message":"Another cutover for this concurrency group started at the same time"}`, wantAPI: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.EscapedPath() != groupPath {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				if body, err := io.ReadAll(r.Body); err != nil || len(body) != 0 {
					t.Errorf("body = %q, error = %v; want empty body", body, err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			group, err := client.StartConcurrencyGroupCutover(context.Background(), "custom", "deploy/production")

			var blocked *ConcurrencyGroupCutoverBlocked
			var apiError *APIError
			switch {
			case test.wantBlocked:
				if !errors.As(err, &blocked) || blocked.QueueObservation.BlockingQueues[0].RecentSourceJobs != 2 || blocked.Migration.BlockingReason != nil {
					t.Fatalf("error = %#v, want blocked readiness", err)
				}
			case test.wantAPI:
				if errors.As(err, &blocked) || !errors.As(err, &apiError) || apiError.Message != "Another cutover for this concurrency group started at the same time" {
					t.Fatalf("error = %#v, want API error", err)
				}
			default:
				if err != nil || group.State != test.wantState {
					t.Fatalf("group = %#v, error = %v", group, err)
				}
			}
		})
	}
}
