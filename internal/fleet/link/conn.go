package link

import "context"

// Conn is one live RPC connection to a peer. *wsclient.Client satisfies it.
type Conn interface {
	// Call issues method and decodes the result into out (may be nil).
	Call(ctx context.Context, method string, params, out any) error
	// Close ends the connection.
	Close() error
	// Done is closed once the connection has died.
	Done() <-chan struct{}
}

// DialFunc opens a connection to the peer hostID over transport t.
type DialFunc func(ctx context.Context, t Transport, hostID string) (Conn, error)
