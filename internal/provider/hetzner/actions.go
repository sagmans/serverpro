package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sagmans/serverpro/internal/poll"
)

const (
	defaultActionTimeout = 10 * time.Minute
	actionPollInterval   = 3 * time.Second
)

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
		if err := poll.Wait(ctx, c.wait, actionPollInterval); err != nil {
			return err
		}
	}
}

func actionContextWithDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultActionTimeout)
}
