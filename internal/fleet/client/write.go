package client

import (
	"context"
	"time"

	"github.com/spacingmind/smind/internal/fleet/wire"
)

// Create calls fleet.task.create. Params are validated before the host is
// resolved, so a bad call never leaves the caller.
func (c *Client) Create(ctx context.Context, ref string, o CallOpts, p wire.TaskCreateParams) (Host, wire.TaskCreateResult, error) {
	var out wire.TaskCreateResult
	if err := p.Validate(); err != nil {
		return Host{}, out, &Error{Code: wire.CodeInvalid, Message: err.Error()}
	}
	h, err := c.call(ctx, ref, o, wire.MethodTaskCreate, func(prov wire.Prov) any {
		p.Prov = prov
		return p
	}, &out)
	return h, out, err
}

// Send calls fleet.task.send.
func (c *Client) Send(ctx context.Context, ref string, o CallOpts, p wire.TaskSendParams) (Host, wire.TaskSendResult, error) {
	var out wire.TaskSendResult
	if err := p.Validate(); err != nil {
		return Host{}, out, &Error{Code: wire.CodeInvalid, Message: err.Error()}
	}
	h, err := c.call(ctx, ref, o, wire.MethodTaskSend, func(prov wire.Prov) any {
		p.Prov = prov
		return p
	}, &out)
	return h, out, err
}

// Stop calls fleet.task.stop.
func (c *Client) Stop(ctx context.Context, ref string, o CallOpts, p wire.TaskStopParams) (Host, wire.TaskStopResult, error) {
	var out wire.TaskStopResult
	if err := p.Validate(); err != nil {
		return Host{}, out, &Error{Code: wire.CodeInvalid, Message: err.Error()}
	}
	h, err := c.call(ctx, ref, o, wire.MethodTaskStop, func(prov wire.Prov) any {
		p.Prov = prov
		return p
	}, &out)
	return h, out, err
}

// Wait calls fleet.run.wait. A zero CallOpts.Timeout is raised above
// wire.MaxRunWait so the client's 15 s default cannot cut the long poll.
func (c *Client) Wait(ctx context.Context, ref string, o CallOpts, p wire.RunWaitParams) (Host, wire.RunWaitResult, error) {
	var out wire.RunWaitResult
	if err := p.Validate(); err != nil {
		return Host{}, out, &Error{Code: wire.CodeInvalid, Message: err.Error()}
	}
	if o.Timeout == 0 {
		o.Timeout = wire.MaxRunWait + 10*time.Second
	}
	h, err := c.call(ctx, ref, o, wire.MethodRunWait, func(prov wire.Prov) any {
		p.Prov = prov
		return p
	}, &out)
	return h, out, err
}
