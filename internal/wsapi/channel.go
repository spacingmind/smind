package wsapi

import (
	"net"
	"net/http"
	"strings"
)

// Channel is how a connection reached the daemon, decided once where the
// connection is created (ADR-0039 section 8). It exists so a credential is
// never accepted over a channel an eavesdropper could read.
type Channel string

const (
	// ChannelLoopback: the /ws request came from a loopback address -- a local
	// client, an ssh tunnel, or a TLS-terminating reverse proxy on this host.
	ChannelLoopback Channel = "loopback"
	// ChannelTLS: the request arrived over TLS (wss).
	ChannelTLS Channel = "tls"
	// ChannelE2EE: a relay-served session (ADR-0035), end-to-end encrypted.
	ChannelE2EE Channel = "e2ee"
	// ChannelPlain: anything else -- a non-loopback peer without TLS. The zero
	// value is treated the same, so an unset channel fails closed.
	ChannelPlain Channel = "plain"
)

// Secure reports whether a credential may cross this channel.
func (c Channel) Secure() bool {
	return c == ChannelLoopback || c == ChannelTLS || c == ChannelE2EE
}

// ChannelOf classifies a /ws upgrade request: TLS first, then a loopback
// peer address, otherwise plain. A loopback peer that says it was forwarded
// from plain http (X-Forwarded-Proto: http, as a same-host proxy in front of
// a plain listener would) is plain: the hop before the proxy is readable.
func ChannelOf(r *http.Request) Channel {
	if r.TLS != nil {
		return ChannelTLS
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ChannelPlain
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return ChannelPlain
	}
	for _, v := range r.Header.Values("X-Forwarded-Proto") {
		if strings.EqualFold(strings.TrimSpace(strings.Split(v, ",")[0]), "http") {
			return ChannelPlain
		}
	}
	return ChannelLoopback
}
