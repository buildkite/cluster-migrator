package buildkite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestOrganizationMigration(t *testing.T) {
	const destination = "11111111-2222-4333-8444-555555555555"
	for _, configure := range []bool{false, true} {
		name := "status"
		if configure {
			name = "configure"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2/organizations/acme/unclustered-to-clustered-migration" {
					t.Errorf("path = %q", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing authorization")
				}
				if configure {
					if r.Method != http.MethodPost {
						t.Errorf("method = %s, want POST", r.Method)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if want := map[string]any{"destination_cluster_id": destination}; !reflect.DeepEqual(body, want) {
						t.Errorf("body = %#v, want %#v", body, want)
					}
				} else if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				_, _ = w.Write([]byte(`{"id":"migration-id","organization_id":"org-id","destination_cluster_id":"` + destination + `","phase":"validate_queues","created_by_id":"creator-id","url":"https://api.buildkite.com/v2/organizations/acme/unclustered-to-clustered-migration","created_at":"2026-10-22T01:00:00.000Z","updated_at":"2026-10-22T02:00:00.000Z"}`))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			var migration *OrganizationMigration
			if configure {
				migration, err = client.ConfigureMigration(context.Background(), destination)
			} else {
				migration, err = client.GetMigration(context.Background())
			}
			if err != nil {
				t.Fatal(err)
			}
			if migration.DestinationClusterID != destination || migration.Phase != "validate_queues" || migration.CreatedByID != "creator-id" || migration.UpdatedAt != "2026-10-22T02:00:00.000Z" || migration.URL == "" {
				t.Fatalf("migration = %#v", migration)
			}
		})
	}
}

func TestMigrationEnrollmentClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"Organization migration enrollment is not available yet"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ConfigureMigration(context.Background(), "11111111-2222-4333-8444-555555555555")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusServiceUnavailable || apiError.Message != "Organization migration enrollment is not available yet" {
		t.Fatalf("error = %v", err)
	}
}
