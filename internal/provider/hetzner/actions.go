package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

var defaultActionTimeout = 10 * time.Minute

func (c Client) WaitAction(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	ctx, cancel := actionContextWithDefaultTimeout(ctx)
	defer cancel()
	for {
		var res struct {
			Action Action `json:"action"`
		}
		if err := c.api.Do(ctx, http.MethodGet, fmt.Sprintf("/actions/%d", id), nil, &res); err != nil {
			return err
		}
		switch res.Action.Status {
		case "success":
			return nil
		case "running":
		case "error":
			if res.Action.Error != nil && res.Action.Error.Message != "" {
				return fmt.Errorf("hetzner action failed: %s", res.Action.Error.Message)
			}
			return fmt.Errorf("hetzner action failed: status=error id=%d", res.Action.ID)
		default:
			return fmt.Errorf("hetzner action unexpected status %q id=%d", res.Action.Status, res.Action.ID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func actionContextWithDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok || defaultActionTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultActionTimeout)
}
