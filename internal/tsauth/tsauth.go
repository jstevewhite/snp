// Package tsauth resolves the identity of API callers.
//
// In production (and in tests) callers reach the API through the Tailscale
// tailnet, so identity is the Tailscale user who owns the connecting node
// (Tailscale). For local development (--dev-listen), Dev returns a fixed
// identity and performs no authentication.
package tsauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

// Identity identifies an authenticated caller.
type Identity struct {
	Login       string // e.g. "alice@example.com"
	DisplayName string // e.g. "Alice Smith"
}

// IdentityResolver resolves a peer's remote address to an Identity.
type IdentityResolver interface {
	WhoIs(ctx context.Context, remoteAddr string) (Identity, error)
}

var (
	_ IdentityResolver = (*Tailscale)(nil)
	_ IdentityResolver = (*Dev)(nil)
)

// whoisClient is the subset of local.Client that Tailscale.WhoIs uses.
// *local.Client satisfies it.
type whoisClient interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

// server is the subset of tsnet functionality that Tailscale uses.
// realServer adapts *tsnet.Server; tests provide a fake.
type server interface {
	// Up brings the node up and returns the tailnet status.
	Up(ctx context.Context) (*ipnstate.Status, error)
	// ListenTLS returns a TLS listener on the tailnet with a
	// Tailscale-issued certificate.
	ListenTLS(network, addr string) (net.Listener, error)
	// LocalClient returns the client used to resolve peer identities.
	LocalClient() (whoisClient, error)
	// Close shuts the node down.
	Close() error
}

// Tailscale is an IdentityResolver backed by an embedded tsnet node. It also
// provides the TLS listener on the tailnet that snp serve accepts on.
type Tailscale struct {
	srv      server
	hostname string
}

// New creates a Tailscale resolver. The node's state (including its node
// key) is persisted under filepath.Join(stateDir, "tsnet"), so authKey is
// only needed for the first join; later starts reuse the persisted node.
func New(stateDir, hostname, authKey string) (*Tailscale, error) {
	if hostname == "" {
		return nil, errors.New("tsauth: hostname must not be empty")
	}
	dir := filepath.Join(stateDir, "tsnet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("tsauth: create state dir %s: %w", dir, err)
	}
	s := &tsnet.Server{
		Dir:      dir,
		Hostname: hostname,
		AuthKey:  authKey,
	}
	return &Tailscale{srv: &realServer{s: s}, hostname: hostname}, nil
}

// Listen brings the node up, fails with a clear error if another node in the
// tailnet already uses this node's name, and returns a TLS listener on the
// tailnet's port 443 using a Tailscale-issued certificate.
//
// It blocks until the node has joined the tailnet: on a first join that is
// when the auth key is consumed or the printed auth URL is approved.
func (t *Tailscale) Listen(ctx context.Context) (net.Listener, error) {
	st, err := t.srv.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("tsauth: join tailnet: %w", err)
	}
	if peer := t.nameCollision(st); peer != "" {
		return nil, fmt.Errorf("tsauth: node name %q is already in use by %s in the tailnet; set a different hostname", t.hostname, peer)
	}
	ln, err := t.srv.ListenTLS("tcp", ":443")
	if err != nil {
		return nil, fmt.Errorf("tsauth: listen: %w", err)
	}
	return ln, nil
}

// WhoIs resolves the identity of the peer at remoteAddr. Failures are
// transient control-plane problems: the caller should treat them as 500 and
// log the details.
func (t *Tailscale) WhoIs(ctx context.Context, remoteAddr string) (Identity, error) {
	lc, err := t.srv.LocalClient()
	if err != nil {
		return Identity{}, fmt.Errorf("tsauth: whois %s: %w", remoteAddr, err)
	}
	res, err := lc.WhoIs(ctx, remoteAddr)
	if err != nil {
		return Identity{}, fmt.Errorf("tsauth: whois %s: %w", remoteAddr, err)
	}
	if res == nil || res.UserProfile == nil {
		return Identity{}, fmt.Errorf("tsauth: whois %s: no user profile in response", remoteAddr)
	}
	return Identity{
		Login:       res.UserProfile.LoginName,
		DisplayName: res.UserProfile.DisplayName,
	}, nil
}

// Close shuts down the tsnet node. Call it once, after the node has been
// started; a second call reports net.ErrClosed.
func (t *Tailscale) Close() error {
	return t.srv.Close()
}

// nameCollision returns the DNS name of a peer that already uses
// t.hostname, or "" if there is no collision. Tailscale allows duplicate
// node names, so this check is what keeps two "snp" nodes from being
// ambiguous.
func (t *Tailscale) nameCollision(st *ipnstate.Status) string {
	if st == nil {
		return ""
	}
	for _, p := range st.Peer {
		if p != nil && strings.EqualFold(p.HostName, t.hostname) {
			if p.DNSName != "" {
				return p.DNSName
			}
			return p.HostName
		}
	}
	return ""
}

// Dev is an IdentityResolver for local development. It performs no
// authentication and always returns a fixed identity.
type Dev struct{}

// NewDev returns a Dev resolver.
func NewDev() *Dev { return &Dev{} }

// WhoIs returns the fixed development identity.
func (d *Dev) WhoIs(ctx context.Context, remoteAddr string) (Identity, error) {
	return Identity{Login: "dev@local", DisplayName: "dev"}, nil
}

// realServer adapts *tsnet.Server to server.
type realServer struct {
	s *tsnet.Server
}

func (r *realServer) Up(ctx context.Context) (*ipnstate.Status, error) {
	return r.s.Up(ctx)
}

func (r *realServer) ListenTLS(network, addr string) (net.Listener, error) {
	return r.s.ListenTLS(network, addr)
}

func (r *realServer) LocalClient() (whoisClient, error) {
	lc, err := r.s.LocalClient()
	if err != nil {
		return nil, err
	}
	return lc, nil // *local.Client implements whoisClient
}

func (r *realServer) Close() error {
	return r.s.Close()
}
