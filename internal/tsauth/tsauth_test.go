package tsauth

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// fakeWhois is a whoisClient with a canned response.
type fakeWhois struct {
	res *apitype.WhoIsResponse
	err error
}

func (f *fakeWhois) WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	return f.res, f.err
}

// fakeServer is a server with canned behavior and call counters.
type fakeServer struct {
	status      *ipnstate.Status
	upErr       error
	listener    net.Listener
	listenErr   error
	listenCalls int
	client      whoisClient
	clientErr   error
	closeCalls  int
}

func (f *fakeServer) Up(ctx context.Context) (*ipnstate.Status, error) {
	return f.status, f.upErr
}

func (f *fakeServer) ListenTLS(network, addr string) (net.Listener, error) {
	f.listenCalls++
	return f.listener, f.listenErr
}

func (f *fakeServer) LocalClient() (whoisClient, error) {
	return f.client, f.clientErr
}

func (f *fakeServer) Close() error {
	f.closeCalls++
	return nil
}

// fakeListener records Close calls.
type fakeListener struct{ closed *bool }

func (l *fakeListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (l *fakeListener) Close() error {
	*l.closed = true
	return nil
}
func (l *fakeListener) Addr() net.Addr { return fakeAddr{} }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "100.64.0.1:443" }

func TestDevWhoIs(t *testing.T) {
	id, err := NewDev().WhoIs(context.Background(), "100.64.0.1:12345")
	if err != nil {
		t.Fatalf("WhoIs: %v", err)
	}
	want := Identity{Login: "dev@local", DisplayName: "dev"}
	if id != want {
		t.Fatalf("got %+v, want %+v", id, want)
	}
}

func TestNew(t *testing.T) {
	stateDir := t.TempDir()
	ts, err := New(stateDir, "snp", "tskey-auth123")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ts.hostname != "snp" {
		t.Errorf("hostname = %q, want %q", ts.hostname, "snp")
	}
	dir := filepath.Join(stateDir, "tsnet")
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("state dir %s: %v", dir, err)
	}
	if !fi.IsDir() {
		t.Errorf("state dir %s is not a directory", dir)
	}
}

func TestNewEmptyHostname(t *testing.T) {
	if _, err := New(t.TempDir(), "", ""); err == nil {
		t.Fatal("New with empty hostname: got nil error, want error")
	}
}

func TestTailscaleWhoIs(t *testing.T) {
	srv := &fakeServer{
		client: &fakeWhois{res: &apitype.WhoIsResponse{
			UserProfile: &tailcfg.UserProfile{
				LoginName:   "alice@example.com",
				DisplayName: "Alice Smith",
			},
		}},
	}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	id, err := ts.WhoIs(context.Background(), "100.64.0.1:12345")
	if err != nil {
		t.Fatalf("WhoIs: %v", err)
	}
	want := Identity{Login: "alice@example.com", DisplayName: "Alice Smith"}
	if id != want {
		t.Fatalf("got %+v, want %+v", id, want)
	}
}

func TestTailscaleWhoIsError(t *testing.T) {
	cause := errors.New("control plane unreachable")
	srv := &fakeServer{client: &fakeWhois{err: cause}}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	_, err := ts.WhoIs(context.Background(), "100.64.0.1:12345")
	if err == nil {
		t.Fatal("WhoIs: got nil error, want error")
	}
	if !errors.Is(err, cause) {
		t.Errorf("error %v does not wrap cause %v", err, cause)
	}
}

func TestTailscaleWhoIsNoProfile(t *testing.T) {
	srv := &fakeServer{client: &fakeWhois{res: &apitype.WhoIsResponse{}}}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	if _, err := ts.WhoIs(context.Background(), "100.64.0.1:12345"); err == nil {
		t.Fatal("WhoIs: got nil error, want error for missing user profile")
	}
}

func TestTailscaleWhoIsLocalClientError(t *testing.T) {
	srv := &fakeServer{clientErr: errors.New("node not started")}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	if _, err := ts.WhoIs(context.Background(), "100.64.0.1:12345"); err == nil {
		t.Fatal("WhoIs: got nil error, want error for local client failure")
	}
}

func TestListenNameCollision(t *testing.T) {
	other := key.NodePublic{}
	srv := &fakeServer{
		status: &ipnstate.Status{
			Peer: map[key.NodePublic]*ipnstate.PeerStatus{
				other: {HostName: "snp", DNSName: "snp.tailnet.ts.net."},
			},
		},
	}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	ln, err := ts.Listen(context.Background())
	if err == nil {
		t.Fatalf("Listen: got nil error, want name collision error (listener=%v)", ln)
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Errorf("error %q does not mention the collision", err)
	}
	if srv.listenCalls != 0 {
		t.Errorf("ListenTLS called %d times on collision, want 0", srv.listenCalls)
	}
}

func TestListenSuccess(t *testing.T) {
	other := key.NodePublic{}
	closed := false
	srv := &fakeServer{
		status: &ipnstate.Status{
			Peer: map[key.NodePublic]*ipnstate.PeerStatus{
				other: {HostName: "other-node", DNSName: "other-node.tailnet.ts.net."},
			},
		},
		listener: &fakeListener{closed: &closed},
	}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	ln, err := ts.Listen(context.Background())
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if ln != srv.listener {
		t.Errorf("Listen returned %v, want the tsnet listener", ln)
	}
	if srv.listenCalls != 1 {
		t.Errorf("ListenTLS called %d times, want 1", srv.listenCalls)
	}
}

func TestListenUpError(t *testing.T) {
	srv := &fakeServer{upErr: errors.New("auth required: open the printed URL")}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	_, err := ts.Listen(context.Background())
	if err == nil {
		t.Fatal("Listen: got nil error, want up error")
	}
	if !errors.Is(err, srv.upErr) {
		t.Errorf("error %v does not wrap up error %v", err, srv.upErr)
	}
}

func TestListenTLSFailure(t *testing.T) {
	srv := &fakeServer{
		status:    &ipnstate.Status{},
		listenErr: errors.New("tsnet: you must enable HTTPS in the admin panel"),
	}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	_, err := ts.Listen(context.Background())
	if err == nil {
		t.Fatal("Listen: got nil error, want ListenTLS error")
	}
}

func TestClose(t *testing.T) {
	srv := &fakeServer{}
	ts := &Tailscale{srv: srv, hostname: "snp"}
	if err := ts.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if srv.closeCalls != 1 {
		t.Errorf("underlying Close called %d times, want 1", srv.closeCalls)
	}
}
