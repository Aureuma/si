package stripebridge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

func BuildCRUDRequest(spec ObjectSpec, op CRUDOp, id string, params map[string]string, idempotencyKey string) (Request, error) {
	if !spec.SupportsOp(op) {
		hint := ""
		if strings.TrimSpace(spec.DeleteHint) != "" && op == CRUDDelete {
			hint = " " + spec.DeleteHint
		}
		return Request{}, fmt.Errorf("%s does not support %s.%s%s", spec.Name, spec.Name, string(op), hint)
	}
	id = strings.TrimSpace(id)
	switch op {
	case CRUDList:
		return Request{Method: http.MethodGet, Path: spec.ListPath, Params: params}, nil
	case CRUDGet:
		if id == "" {
			return Request{}, fmt.Errorf("id is required for %s get", spec.Name)
		}
		return Request{Method: http.MethodGet, Path: fmt.Sprintf(spec.ResourcePath, id), Params: params}, nil
	case CRUDCreate:
		return Request{
			Method:         http.MethodPost,
			Path:           spec.ListPath,
			Params:         params,
			IdempotencyKey: strings.TrimSpace(idempotencyKey),
		}, nil
	case CRUDUpdate:
		if id == "" {
			return Request{}, fmt.Errorf("id is required for %s update", spec.Name)
		}
		return Request{
			Method:         http.MethodPost,
			Path:           fmt.Sprintf(spec.ResourcePath, id),
			Params:         params,
			IdempotencyKey: strings.TrimSpace(idempotencyKey),
		}, nil
	case CRUDDelete:
		if id == "" {
			return Request{}, fmt.Errorf("id is required for %s delete", spec.Name)
		}
		return Request{
			Method:         http.MethodDelete,
			Path:           fmt.Sprintf(spec.ResourcePath, id),
			Params:         params,
			IdempotencyKey: strings.TrimSpace(idempotencyKey),
		}, nil
	default:
		return Request{}, fmt.Errorf("unsupported operation: %s", op)
	}
}

func (c *Client) ExecuteCRUD(ctx context.Context, spec ObjectSpec, op CRUDOp, id string, params map[string]string, idempotencyKey string) (Response, error) {
	req, err := BuildCRUDRequest(spec, op, id, params, idempotencyKey)
	if err != nil {
		return Response{}, err
	}
	return c.Do(ctx, req)
}
