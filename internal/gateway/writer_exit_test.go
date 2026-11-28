package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// stalledStream is a sync stream whose every Send blocks until release
// closes, as a send on a transport out of flow-control credit blocks until
// the stream is done; Recv yields what recv carries and io.EOF once it
// closes. Sync calls nothing else of it.
type stalledStream struct {
	grpc.ServerStream
	ctx     context.Context
	recv    chan *innerwallv1.SyncRequest
	sending chan struct{}
	release chan struct{}
}

func newStalledStream(t *testing.T, id identity.WorkloadID) *stalledStream {
	t.Helper()
	s := &stalledStream{
		ctx:     WithIdentity(context.Background(), id),
		recv:    make(chan *innerwallv1.SyncRequest, 1),
		sending: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	s.recv <- &innerwallv1.SyncRequest{Msg: &innerwallv1.SyncRequest_Hello{Hello: &innerwallv1.Hello{Agent: &innerwallv1.AgentInfo{Version: "test"}}}}
	return s
}

func (s *stalledStream) Context() context.Context { return s.ctx }

func (s *stalledStream) Recv() (*innerwallv1.SyncRequest, error) {
	m, ok := <-s.recv
	if !ok {
		return nil, io.EOF
	}
	return m, nil
}

func (s *stalledStream) Send(*innerwallv1.SyncResponse) error {
	select {
	case s.sending <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

// awaitSending waits until the writer is inside a Send.
func (s *stalledStream) awaitSending(t *testing.T) {
	t.Helper()
	select {
	case <-s.sending:
	case <-time.After(5 * time.Second):
		t.Fatal("the writer never began a send")
	}
}

// quietRegistry accepts every write a stream's open and end make, and
// reports each offline mark on marked.
type quietRegistry struct {
	registry.Store
	marked chan struct{}
}

func (quietRegistry) RecordAgent(context.Context, identity.WorkloadID, registry.AgentInfo, uint64, *registry.PresentedCredential, time.Time) error {
	return nil
}

func (quietRegistry) RecordStreamOpen(context.Context, identity.WorkloadID, uuid.UUID, time.Time) error {
	return nil
}

func (quietRegistry) RecordSnapshotSent(context.Context, identity.WorkloadID, time.Time) error {
	return nil
}

func (r quietRegistry) MarkStreamOffline(context.Context, identity.WorkloadID, uuid.UUID, time.Time) (bool, error) {
	select {
	case r.marked <- struct{}{}:
	default:
	}
	return true, nil
}

type onePolicy struct{}

func (onePolicy) GetWorkloadPolicy(context.Context, identity.WorkloadID) (*innerwallv1.WorkloadPolicy, error) {
	return &innerwallv1.WorkloadPolicy{Version: 1}, nil
}

func newStalledServer(t *testing.T) (*Server, *records, quietRegistry) {
	t.Helper()
	rec := &records{}
	reg := quietRegistry{marked: make(chan struct{}, 1)}
	return New(Deps{Registry: reg, Policies: onePolicy{}, Engine: &compiler.Engine{}, Log: slog.New(rec)}), rec, reg
}

// serve runs the handler over stream and returns where it reports.
func serve(s *Server, stream *stalledStream) <-chan error {
	done := make(chan error, 1)
	go func() { done <- s.Sync(stream) }()
	return done
}

// The handler returns only after its writer has: a Send still in progress
// when the stream ends is waited for, so nothing the writer does outlasts
// the handler that shutdown waits on (#260).
func TestSyncHandlerWaitsForItsWriter(t *testing.T) {
	s, rec, _ := newStalledServer(t)
	stream := newStalledStream(t, mustID(t))
	done := serve(s, stream)
	stream.awaitSending(t)

	close(stream.recv)
	select {
	case err := <-done:
		t.Fatalf("the handler returned (%v) while its writer was still in a send", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(stream.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handler returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the handler did not return once its writer's send did")
	}
	if n := rec.count(slog.LevelWarn); n != 0 {
		t.Fatalf("%d warnings for a writer that returned within the bound", n)
	}
}

// A Send that does not return while the handler waits (a transport out of
// flow-control credit releases it only once the stream is done, which is
// after the handler returns) bounds the wait: the handler warns, writes
// the offline mark, and returns, rather than hang (#260).
func TestSyncHandlerBoundsItsWaitForAStalledWriter(t *testing.T) {
	s, rec, reg := newStalledServer(t)
	stream := newStalledStream(t, mustID(t))
	t.Cleanup(func() { close(stream.release) })
	done := serve(s, stream)
	stream.awaitSending(t)

	close(stream.recv)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handler returned %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the handler hung on a writer stalled in a send")
	}
	rec.wait(t, slog.LevelWarn, "sync stream writer still in a send")
	select {
	case <-reg.marked:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream's offline mark was not written")
	}
}
