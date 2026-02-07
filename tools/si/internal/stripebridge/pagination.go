package stripebridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type listEnvelope struct {
	Object  string           `json:"object"`
	Data    []map[string]any `json:"data"`
	HasMore bool             `json:"has_more"`
}

func (c *Client) ListAll(ctx context.Context, path string, params map[string]string, limit int) ([]map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("stripe client is not initialized")
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 0 {
		limit = 10000
	}
	const pageMax = 100
	out := make([]map[string]any, 0, minInt(limit, 128))
	cursor := ""

	for {
		pageParams := map[string]string{}
		for key, value := range params {
			pageParams[key] = value
		}
		pageSize := pageMax
		if limit > 0 {
			remaining := limit - len(out)
			if remaining <= 0 {
				break
			}
			if remaining < pageSize {
				pageSize = remaining
			}
		}
		if _, ok := pageParams["limit"]; !ok {
			pageParams["limit"] = strconv.Itoa(pageSize)
		}
		if strings.TrimSpace(cursor) != "" {
			pageParams["starting_after"] = cursor
		}
		resp, err := c.Do(ctx, Request{
			Method: "GET",
			Path:   path,
			Params: pageParams,
		})
		if err != nil {
			return nil, err
		}
		if resp.Data == nil {
			return nil, fmt.Errorf("stripe list response missing json body")
		}
		env := listEnvelope{}
		if err := decodeMap(resp.Data, &env); err != nil {
			return nil, fmt.Errorf("decode list response: %w", err)
		}
		if len(env.Data) == 0 {
			break
		}
		out = append(out, env.Data...)
		last := env.Data[len(env.Data)-1]
		lastID, _ := stringField(last, "id")
		cursor = strings.TrimSpace(lastID)
		if !env.HasMore || cursor == "" {
			break
		}
	}

	if limit > 0 && len(out) > limit {
		return out[:limit], nil
	}
	return out, nil
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
