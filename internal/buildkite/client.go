package buildkite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Client struct {
	baseURL               string
	organization          string
	organizationWasCached bool
	organizationCachePath string
	token                 string
	httpClient            *http.Client
}

type Cluster struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type organization struct {
	Slug string `json:"slug"`
}

func NewClient(baseURL, organization, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("API URL must include a scheme and host")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(parsed.Path, "/v2") {
		parsed.Path += "/v2"
	}
	cacheDir, _ := os.UserCacheDir()
	cacheKey := sha256.Sum256([]byte(parsed.String() + "\x00" + token))
	var organizationCachePath string
	if cacheDir != "" {
		organizationCachePath = filepath.Join(cacheDir, "cluster-migrator", fmt.Sprintf("%x.organization", cacheKey))
	}

	return &Client{
		baseURL:               strings.TrimRight(parsed.String(), "/"),
		organization:          organization,
		organizationCachePath: organizationCachePath,
		token:                 token,
		httpClient:            httpClient,
	}, nil
}

func (c *Client) ResolveOrganization(ctx context.Context) error {
	if c.organization != "" {
		return nil
	}
	if cached, err := os.ReadFile(c.organizationCachePath); err == nil {
		if organization := strings.TrimSpace(string(cached)); organization != "" {
			c.organization = organization
			c.organizationWasCached = true
			return nil
		}
	}
	return c.discoverOrganization(ctx)
}

func (c *Client) discoverOrganization(ctx context.Context) error {
	var organizations []organization
	if err := c.do(ctx, http.MethodGet, "/organizations?per_page=2", nil, &organizations); err != nil {
		return fmt.Errorf("discover organization from API token: %w", err)
	}
	if len(organizations) != 1 {
		return fmt.Errorf("API token grants access to %d organizations, want exactly 1", len(organizations))
	}
	if organizations[0].Slug == "" {
		return fmt.Errorf("organization returned by API has no slug")
	}
	c.organization = organizations[0].Slug
	c.organizationWasCached = false
	c.writeOrganizationCache()
	return nil
}

func (c *Client) writeOrganizationCache() {
	if c.organizationCachePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.organizationCachePath), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(c.organizationCachePath, []byte(c.organization+"\n"), 0o600)
}

func (c *Client) ResolveCluster(ctx context.Context, identifier string) (*Cluster, error) {
	clusters, err := c.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	var match *Cluster
	for i := range clusters {
		if clusters[i].ID != identifier && clusters[i].Name != identifier {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("multiple clusters match %q", identifier)
		}
		match = &clusters[i]
	}
	if match == nil {
		return nil, fmt.Errorf("no cluster found matching %q", identifier)
	}
	return match, nil
}

func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	path := c.path("clusters") + "?per_page=100"
	var clusters []Cluster
	for path != "" {
		page, err := offsetPage[Cluster](ctx, c, path)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, page.Items...)
		path = page.Next
	}
	return clusters, nil
}

func (c *Client) path(parts ...string) string {
	segments := []string{"organizations", c.organization}
	segments = append(segments, parts...)
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return "/" + strings.Join(segments, "/")
}

func (c *Client) do(ctx context.Context, method, path string, requestBody, responseBody any) error {
	_, err := c.doWithHeaders(ctx, method, path, requestBody, responseBody)
	return err
}

func (c *Client) doWithHeaders(ctx context.Context, method, path string, requestBody, responseBody any) (http.Header, error) {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	requestURL := c.baseURL + path
	parsedPath, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse request URL: %w", err)
	}
	if parsedPath.IsAbs() {
		base, err := url.Parse(c.baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse API URL: %w", err)
		}
		if parsedPath.Scheme != base.Scheme || parsedPath.Host != base.Host {
			return nil, fmt.Errorf("refusing to send credentials to pagination URL %q", path)
		}
		requestURL = parsedPath.String()
	}

	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound && c.organizationWasCached {
		oldPrefix := "/organizations/" + url.PathEscape(c.organization) + "/"
		if strings.HasPrefix(path, oldPrefix) {
			c.organization = ""
			c.organizationWasCached = false
			_ = os.Remove(c.organizationCachePath)
			if err := c.discoverOrganization(ctx); err != nil {
				return nil, err
			}
			path = strings.Replace(path, oldPrefix, "/organizations/"+url.PathEscape(c.organization)+"/", 1)
			return c.doWithHeaders(ctx, method, path, requestBody, responseBody)
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeAPIError(response)
	}
	if responseBody == nil || response.StatusCode == http.StatusNoContent {
		return response.Header.Clone(), nil
	}
	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return response.Header.Clone(), nil
}

func nextLink(header string) string {
	for _, link := range strings.Split(header, ",") {
		parts := strings.Split(link, ";")
		for _, parameter := range parts[1:] {
			if strings.TrimSpace(parameter) == `rel="next"` {
				return strings.Trim(strings.TrimSpace(parts[0]), "<>")
			}
		}
	}
	return ""
}
