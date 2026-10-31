package quota

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const detailNoInstanceKey = "no API key set"

// InstanceCreds resolves what a usage call for one provider instance needs. It
// runs inside the daemon; the key never leaves it.
type InstanceCreds interface {
	Creds(instanceID string) (quotaURL, apiKey string, err error)
}

var (
	// ErrInstanceUnsupported means the instance has no usage source.
	ErrInstanceUnsupported = errors.New("quota: instance has no usage source")
	// ErrInstanceNoKey means the instance has no API key set.
	ErrInstanceNoKey = errors.New("quota: instance has no api key")
)

type zaiInstanceFetcher struct {
	creds InstanceCreds
	base  *http.Client
	now   func() time.Time
}

// NewZaiInstanceFetcher returns an InstanceFetcher for Z.AI provider
// instances. base may be nil.
func NewZaiInstanceFetcher(creds InstanceCreds, base *http.Client) InstanceFetcher {
	if base == nil {
		base = &http.Client{}
	}
	return &zaiInstanceFetcher{creds: creds, base: base, now: time.Now}
}

func (f *zaiInstanceFetcher) Fetch(ctx context.Context, instanceID string) (Report, error) {
	now := f.now()
	quotaURL, apiKey, err := f.creds.Creds(instanceID)
	switch {
	case errors.Is(err, ErrInstanceUnsupported):
		return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailUnsupport}, now), nil
	case errors.Is(err, ErrInstanceNoKey):
		return unavailableReport(Problem{Kind: ProblemNoQuota, Detail: detailNoInstanceKey}, now), nil
	case err != nil:
		// Never include err text: it may carry credential material.
		return errorReport("error", now), &FetchError{msg: "error"}
	}
	return FetchZai(ctx, f.base, quotaURL, apiKey)
}
