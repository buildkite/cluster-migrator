package buildkite

import (
	"context"
	"net/http"
)

type CursorPaginated[T any] struct {
	MigrationConfigured *bool `json:"-"`
	Items               []T   `json:"items"`
	Links               struct {
		Next string `json:"next"`
	} `json:"links"`
}

type OffsetPaginated[T any] struct {
	Items []T
	Next  string
}

func cursorPage[T any](ctx context.Context, client *Client, path string) (CursorPaginated[T], error) {
	var page CursorPaginated[T]
	header, err := client.doWithHeaders(ctx, http.MethodGet, path, nil, &page)
	switch header.Get("Buildkite-Migration-Configured") {
	case "true":
		configured := true
		page.MigrationConfigured = &configured
	case "false":
		configured := false
		page.MigrationConfigured = &configured
	}
	return page, err
}

func offsetPage[T any](ctx context.Context, client *Client, path string) (OffsetPaginated[T], error) {
	var page OffsetPaginated[T]
	header, err := client.doWithHeaders(ctx, http.MethodGet, path, nil, &page.Items)
	if err != nil {
		return page, err
	}
	page.Next = nextLink(header.Get("Link"))
	return page, nil
}
