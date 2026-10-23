package ghctx

import (
	"context"
	"errors"

	"github.com/spacingmind/smind/internal/forge"
)

func (c *Client) PRDetails(context.Context, string, int) (forge.PRDetails, error) {
	return forge.PRDetails{}, errors.New("not implemented")
}

func (c *Client) CheckLog(context.Context, string, int64, int64) (string, error) {
	return "", errors.New("not implemented")
}
