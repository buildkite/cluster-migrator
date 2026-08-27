package buildkite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	baseURL      string
	organization string
	token        string
	httpClient   *http.Client
}

type Cluster struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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

	return &Client{
		baseURL:      strings.TrimRight(parsed.String(), "/"),
		organization: organization,
		token:        token,
		httpClient:   httpClient,
	}, nil
}

func (c *Client) ResolveCluster(ctx context.Context, identifier string) (*Cluster, error) {
	var clusters []Cluster
	if err := c.do(ctx, http.MethodGet, c.path("clusters")+"?per_page=100", nil, &clusters); err != nil {
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

func (c *Client) path(parts ...string) string {
	segments := []string{"organizations", c.organization}
	segments = append(segments, parts...)
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return "/" + strings.Join(segments, "/")
}

func (c *Client) do(ctx context.Context, method, path string, requestBody, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	requestURL := c.baseURL + path
	parsedPath, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("parse request URL: %w", err)
	}
	if parsedPath.IsAbs() {
		base, err := url.Parse(c.baseURL)
		if err != nil {
			return fmt.Errorf("parse API URL: %w", err)
		}
		if parsedPath.Scheme != base.Scheme || parsedPath.Host != base.Host {
			return fmt.Errorf("refusing to send credentials to pagination URL %q", path)
		}
		requestURL = parsedPath.String()
	}

	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAPIError(response)
	}
	if responseBody == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
