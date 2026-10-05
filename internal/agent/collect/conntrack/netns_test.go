//go:build netns && linux

package conntrack

// The collection namespace suite runs the conntrack source against a real
// kernel. Each test runs itself again under `unshare --net`, so it owns a
// fresh network namespace: a veth pair, both ends in the namespace, carries two local addresses,
// traffic between them is tracked, and a throwaway table with a
// connection-tracking rule switches tracking on in the namespace (the
// kernel tracks nothing in a namespace until a rule needs it). It needs
// root, nft, and unshare; `make test-netns` runs it beside the
// enforcement suite.

import (
	"context"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ct "github.com/ti-mo/conntrack"
	"github.com/vishvananda/netlink"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

const (
	envInNamespace = "INNERWALL_COLLECT_NETNS"
	serverIP       = "10.77.0.1"
	clientIP       = "10.77.0.2"
)

// inNamespace reports whether the test is running inside its namespace,
// set up. Outside, it runs the test again under unshare, relays the
// result, and returns false so the caller returns.
func inNamespace(t *testing.T) bool {
	t.Helper()
	if os.Getenv(envInNamespace) == "1" {
		setUpNamespace(t)
		return true
	}
	for _, bin := range []string{"unshare", "nft"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("%s is required for the collection namespace suite", bin)
		}
	}
	cmd := exec.Command("unshare", "--net", "--", os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v", "-test.count=1") //nolint:gosec // this test binary, run again
	cmd.Env = append(os.Environ(), envInNamespace+"=1")
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		t.Log(line)
	}
	if err != nil {
		t.Fatalf("in the namespace: %v", err)
	}
	return false
}

func setUpNamespace(t *testing.T) {
	t.Helper()
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		t.Fatal(err)
	}
	pair := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "iw0"}, PeerName: "iw1"}
	if err := netlink.LinkAdd(pair); err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]string{"iw0": serverIP, "iw1": clientIP} {
		link, err := netlink.LinkByName(name)
		if err != nil {
			t.Fatal(err)
		}
		addr, err := netlink.ParseAddr(a + "/24")
		if err != nil {
			t.Fatal(err)
		}
		if err := netlink.AddrAdd(link, addr); err != nil {
			t.Fatal(err)
		}
		if err := netlink.LinkSetUp(link); err != nil {
			t.Fatal(err)
		}
	}
	nft := exec.Command("nft", "-f", "-")
	nft.Stdin = strings.NewReader("table inet collecttest {\n\tchain in {\n\t\ttype filter hook input priority 0; policy accept;\n\t\tct state new counter\n\t}\n}\n")
	if out, err := nft.CombinedOutput(); err != nil {
		t.Fatalf("switching tracking on: %v: %s", err, out)
	}
	_ = os.WriteFile(acctPath, []byte("1"), 0o644) //nolint:gosec // a sysctl
}

func namespaceSource() *Source {
	return &Source{
		Gaps:           &collect.Gaps{},
		LocalAddresses: func() []netip.Addr { return []netip.Addr{netip.MustParseAddr(serverIP), netip.MustParseAddr(clientIP)} },
	}
}

// server accepts connections on serverIP:port and holds them open until
// the test ends.
func server(t *testing.T, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(serverIP, strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range held {
			_ = c.Close()
		}
		mu.Unlock()
	})
}

func connect(t *testing.T, port int) net.Conn {
	t.Helper()
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(clientIP)}, Timeout: 5 * time.Second}
	c, err := d.Dial("tcp", net.JoinHostPort(serverIP, strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	return c
}

func onPort(port int) func(collect.Observation) bool {
	return func(o collect.Observation) bool {
		return o.DstPort == uint16(port) && o.Protocol == innerwallv1.Protocol_PROTOCOL_TCP && //nolint:gosec // test ports
			o.Src == netip.MustParseAddr(clientIP) && o.Dst == netip.MustParseAddr(serverIP)
	}
}

// recorder keeps every observation, and can block emit to stall the
// source.
type recorder struct {
	mu   sync.Mutex
	all  []collect.Observation
	gate chan struct{}
}

func (r *recorder) add(o collect.Observation) {
	r.mu.Lock()
	gate := r.gate
	r.mu.Unlock()
	if gate != nil {
		<-gate
	}
	r.mu.Lock()
	r.all = append(r.all, o)
	r.mu.Unlock()
}

func (r *recorder) connections(match func(collect.Observation) bool) (conns uint64, seen int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.all {
		if match(o) {
			conns += o.Connections
			seen++
		}
	}
	return conns, seen
}

func (r *recorder) waitFor(t *testing.T, what string, match func(collect.Observation) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, n := r.connections(match); n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no observation: %s", what)
}

// destroy deletes the table entries of connections to serverIP:port.
func destroy(t *testing.T, port int) {
	t.Helper()
	c, err := ct.Dial(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	flows, err := c.Dump(nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted := 0
	for _, f := range flows {
		if f.TupleOrig.IP.DestinationAddress == netip.MustParseAddr(serverIP) && f.TupleOrig.Proto.DestinationPort == uint16(port) { //nolint:gosec // test ports
			if err := c.Delete(f); err != nil {
				t.Fatal(err)
			}
			deleted++
		}
	}
	if deleted == 0 {
		t.Fatalf("no table entry for port %d", port)
	}
}

// TestDumpAtStartInNamespace opens a connection before the source
// subscribes and holds it open: it produces no NEW event the source can
// see, so only the dump can find it, and it must appear, counted once, in
// the first window the collector closes.
func TestDumpAtStartInNamespace(t *testing.T) {
	if !inNamespace(t) {
		return
	}
	const port = 7001
	server(t, port)
	c := connect(t, port)
	defer func() { _ = c.Close() }()

	buf := collect.NewBuffer(0)
	coll := &collect.Collector{Source: namespaceSource(), Buffer: buf}
	coll.SetConfig(&innerwallv1.SyncConfig{FlowAggregationWindowSeconds: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	go func() { _ = coll.Run(ctx) }()

	popCtx, popCancel := context.WithTimeout(ctx, 10*time.Second)
	defer popCancel()
	w, ok := buf.Pop(popCtx)
	if !ok {
		t.Fatal("no window closed")
	}
	if w.Start.After(started.Add(time.Second)) {
		t.Fatalf("first closed window starts at %v, more than a window after the collector started at %v", w.Start, started)
	}
	for _, r := range w.Records {
		if r.GetDstPort() == port && r.GetSrcAddress() == clientIP && r.GetDstAddress() == serverIP {
			if r.GetConnectionCount() != 1 {
				t.Fatalf("connection open before the subscribe counted %d times", r.GetConnectionCount())
			}
			return
		}
	}
	t.Fatalf("connection open before the subscribe missing from the first window: %v", w.Records)
}

// TestListenDumpRaceInNamespace opens a connection after the source
// listens and before it dumps, the one instant in which a connection is
// both a NEW event and a dump entry, and checks that it is counted once,
// and that its close adds bytes without counting it again.
func TestListenDumpRaceInNamespace(t *testing.T) {
	if !inNamespace(t) {
		return
	}
	const port = 7002
	server(t, port)
	var c net.Conn
	s := namespaceSource()
	s.afterListen = func() {
		c = connect(t, port)
		// Let the kernel confirm the entry, so the dump lists it.
		time.Sleep(100 * time.Millisecond)
	}
	r := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.Run(ctx, r.add) }()

	r.waitFor(t, "the connection opened between listen and dump", onPort(port))
	time.Sleep(time.Second) // the NEW and the dump have both been read
	if conns, _ := r.connections(onPort(port)); conns != 1 {
		t.Fatalf("connection opened between listen and dump counted %d times", conns)
	}
	_ = c.Close()
	// A closed TCP connection lingers in the table; deleting its entry
	// ends it now, and the kernel reports that as its DESTROY.
	destroy(t, port)
	r.waitFor(t, "the close", func(o collect.Observation) bool { return onPort(port)(o) && o.Bytes > 0 })
	if conns, _ := r.connections(onPort(port)); conns != 1 {
		t.Fatalf("after its close the connection counts %d times", conns)
	}
}

// TestOverrunRecoveredInNamespace shrinks the event socket to the
// kernel's minimum and stalls the source while connections churn, so the
// kernel drops events and reports it. The source must count the overrun,
// record an overrun gap, subscribe again without returning, and go on
// observing.
func TestOverrunRecoveredInNamespace(t *testing.T) {
	if !inNamespace(t) {
		return
	}
	const port, after = 7003, 7004
	server(t, port)
	server(t, after)
	s := namespaceSource()
	s.ReadBuffer = 1 // the kernel raises it to its minimum
	s.queue = 1
	r := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- s.Run(ctx, r.add) }()

	// One observation proves the subscription is up; then stall it.
	first := connect(t, port)
	r.waitFor(t, "a connection before the stall", onPort(port))
	r.mu.Lock()
	r.gate = make(chan struct{})
	r.mu.Unlock()
	_ = first.Close()
	for range 500 {
		c := connect(t, port)
		_ = c.Close()
	}
	r.mu.Lock()
	close(r.gate)
	r.gate = nil
	r.mu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	for s.Gaps.Overruns() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no overrun counted after stalling a minimum-sized socket")
		}
		select {
		case err := <-returned:
			t.Fatalf("Run returned: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Recovered: a connection after the overrun is observed.
	deadline = time.Now().Add(10 * time.Second)
	for {
		c := connect(t, after)
		_ = c.Close()
		if _, n := r.connections(onPort(after)); n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nothing observed after the overrun; the source did not recover")
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case err := <-returned:
		t.Fatalf("Run returned on an overrun: %v", err)
	default:
	}
	var overrun bool
	for _, g := range s.Gaps.Pending() {
		if g.Kind == collect.GapSourceOverrun && g.Source == collect.GapConntrack && !g.To.Before(g.From) {
			overrun = true
		}
	}
	if !overrun {
		t.Fatalf("no overrun gap recorded: %+v", s.Gaps.Pending())
	}
}
