package buildkite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOrganizationCachesTokenOrganization(t *testing.T) {
	cacheDir := t.TempDir()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v2/organizations" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v2/organizations")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"slug":"acme"}]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.organizationCachePath = filepath.Join(cacheDir, "organization")
	if err := client.ResolveOrganization(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := client.organization, "acme"; got != want {
		t.Fatalf("organization = %q, want %q", got, want)
	}

	cachedClient, err := NewClient(server.URL, "", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	cachedClient.organizationCachePath = filepath.Join(cacheDir, "organization")
	if err := cachedClient.ResolveOrganization(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := cachedClient.organization, "acme"; got != want {
		t.Fatalf("cached organization = %q, want %q", got, want)
	}
	if got, want := requests, 1; got != want {
		t.Fatalf("organization requests = %d, want %d", got, want)
	}
}

func TestResolveOrganizationRejectsUnexpectedOrganizationCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.organizationCachePath = filepath.Join(t.TempDir(), "organization")
	if err := client.ResolveOrganization(context.Background()); err == nil {
		t.Fatal("ResolveOrganization() error = nil, want organization count error")
	}
}

func TestCachedOrganizationIsRediscoveredAfterNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/old/clusters":
			http.NotFound(w, r)
		case "/v2/organizations":
			_, _ = w.Write([]byte(`[{"slug":"acme"}]`))
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "organization")
	if err := os.WriteFile(cachePath, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(server.URL, "", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.organizationCachePath = cachePath
	if err := client.ResolveOrganization(context.Background()); err != nil {
		t.Fatal(err)
	}
	clusters, err := client.ListClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := clusters[0].Name, "production"; got != want {
		t.Fatalf("cluster name = %q, want %q", got, want)
	}
	if cached, err := os.ReadFile(cachePath); err != nil {
		t.Fatal(err)
	} else if got, want := string(cached), "acme\n"; got != want {
		t.Fatalf("cached organization = %q, want %q", got, want)
	}
}
