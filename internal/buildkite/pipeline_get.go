package buildkite

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

func (c *Client) GetPipeline(ctx context.Context, pipeline string) (*Pipeline, error) {
	var result Pipeline
	if err := c.do(ctx, http.MethodGet, c.pipelinePath(pipeline), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ResolvePipelineIdentifier(ctx context.Context, identifier string) (*Pipeline, error) {
	query := url.Values{"per_page": []string{"100"}}
	identifierIsID := isUUID(identifier)
	if !identifierIsID {
		query.Set("name", identifier)
	}
	path := c.path("pipelines") + "?" + query.Encode()
	var matches []Pipeline
	for path != "" {
		page, err := offsetPage[Pipeline](ctx, c, path)
		if err != nil {
			return nil, err
		}
		for _, pipeline := range page.Items {
			if identifierIsID && strings.EqualFold(pipeline.ID, identifier) {
				return &pipeline, nil
			}
			if !identifierIsID && pipeline.Name == identifier {
				matches = append(matches, pipeline)
			}
		}
		path = page.Next
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no pipeline found matching ID, name, or slug %q", identifier)
	}
	if len(matches) > 1 {
		slugs := make([]string, len(matches))
		for i, pipeline := range matches {
			slugs[i] = pipeline.Slug
		}
		sort.Strings(slugs)
		return nil, fmt.Errorf("multiple pipelines match name %q; use an ID or slug: %s", identifier, strings.Join(slugs, ", "))
	}
	return &matches[0], nil
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
