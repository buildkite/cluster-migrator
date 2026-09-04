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

func (c *Client) ResolvePipelineName(ctx context.Context, identifier string) (*Pipeline, error) {
	query := url.Values{"name": []string{identifier}, "per_page": []string{"100"}}
	path := c.path("pipelines") + "?" + query.Encode()
	var matches []Pipeline
	for path != "" {
		page, err := offsetPage[Pipeline](ctx, c, path)
		if err != nil {
			return nil, err
		}
		for _, pipeline := range page.Items {
			if pipeline.Name == identifier {
				matches = append(matches, pipeline)
			}
		}
		path = page.Next
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no pipeline found matching name or slug %q", identifier)
	}
	if len(matches) > 1 {
		slugs := make([]string, len(matches))
		for i, pipeline := range matches {
			slugs[i] = pipeline.Slug
		}
		sort.Strings(slugs)
		return nil, fmt.Errorf("multiple pipelines match name %q; use a slug: %s", identifier, strings.Join(slugs, ", "))
	}
	return &matches[0], nil
}
