package buildkite

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

func (c *Client) GetPipeline(ctx context.Context, identifier string) (*Pipeline, error) {
	var pipeline Pipeline
	if err := c.do(ctx, http.MethodGet, c.path("pipelines", identifier), nil, &pipeline); err != nil {
		return nil, err
	}
	if pipeline.Slug == "" {
		return nil, fmt.Errorf("pipeline response missing slug")
	}
	return &pipeline, nil
}

type pipelineLookup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (c *Client) ResolvePipelineSlug(ctx context.Context, identifier string) (string, error) {
	query := url.Values{"per_page": []string{"100"}}
	identifierIsID := isUUID(identifier)
	if !identifierIsID {
		query.Set("name", identifier)
	}
	path := c.path("pipelines") + "?" + query.Encode()
	var matches []pipelineLookup
	for path != "" {
		page, err := offsetPage[pipelineLookup](ctx, c, path)
		if err != nil {
			return "", err
		}
		for _, pipeline := range page.Items {
			if identifierIsID && strings.EqualFold(pipeline.ID, identifier) {
				return pipeline.Slug, nil
			}
			if pipeline.Name == identifier {
				matches = append(matches, pipeline)
			}
		}
		path = page.Next
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no pipeline found matching ID, name, or slug %q", identifier)
	}
	if len(matches) > 1 {
		slugs := make([]string, len(matches))
		for i, pipeline := range matches {
			slugs[i] = pipeline.Slug
		}
		sort.Strings(slugs)
		return "", fmt.Errorf("multiple pipelines match name %q; use an ID or slug: %s", identifier, strings.Join(slugs, ", "))
	}
	return matches[0].Slug, nil
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return false
		}
	}
	return true
}
