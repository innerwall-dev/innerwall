//go:build netns

package nft_test

// The network-namespace suite exercises enforcement against a real kernel:
// a veth pair joins this process's namespace (the peer) to a fresh one
// (the target), the target runs the agent's enforcement store, conntrack
// source, and log source against real nftables, and the peer sends real
// traffic. It needs root, nft, and nsenter, and is selected by the netns
// build tag so the ordinary test job is unaffected; `make test-netns`
// runs it locally and the enforcement CI job runs it on a runner.
//
// One test binary plays every part. Each parent test creates its own
// namespace (a holder process started with a new network namespace keeps
// it alive), builds the veth pair, and runs itself again inside the
// namespace for each phase, driving it over stdin/stdout.
//
// A fresh namespace has no connection tracking until a loaded ruleset
// references it, so each namespace is also a host on which nothing but
// the agent engages connection tracking. The visibility test depends on
// that and installs nothing beside the owned table.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ct "github.com/ti-mo/conntrack"
	"github.com/vishvananda/netlink"

	"github.com/innerwall-dev/innerwall/internal/agent/collect"
	"github.com/innerwall-dev/innerwall/internal/agent/collect/conntrack"
	"github.com/innerwall-dev/innerwall/internal/agent/collect/nflog"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	"github.com/innerwall-dev/innerwall/internal/agent/enforce/nft"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
)

const (
	envRole     = "INNERWALL_NETNS_ROLE"
	envStateDir = "INNERWALL_NETNS_STATE_DIR"

	vethTarget = "iwveth0"
	vethPeer   = "iwveth1"
	targetIP   = "10.99.0.1"
	peerIP     = "10.99.0.2"
	peer2IP    = "10.99.0.3"
	// Two peers whose connections are held open across policy changes.
	heldIP        = "10.99.0.4"
	heldRemovedIP = "10.99.0.5"

	portAllowed = 18080
	portDenied  = 19090

	ruleA = "0191e5c0-0000-7000-8000-00000000000a/tcp"
	// ruleEarly sorts before ruleA: adding it is what used to renumber
	// ruleA.
	ruleEarly = "0191e5c0-0000-7000-8000-000000000001/tcp"

	nflogGroup = 201
	tableName  = "innerwalltest"

	// A pre-existing user of the connection mark: another table, at a hook
	// that runs before ours, writing the low bits of every new
	// connection. Ours must leave it intact and still attribute.
	foreignTable = "iwforeign"
	foreignMark  = 0x2a
)

// foreignScript installs the foreign table; it is not ours and is never
// touched by the agent.
const foreignScript = "table inet " + foreignTable + " {}\ndelete table inet " + foreignTable + "\ntable inet " + foreignTable + " {\n\tchain premark {\n\t\ttype filter hook prerouting priority mangle; policy accept;\n\t\tct state new ct mark set 0x2a\n\t}\n}\n"

type outcome string

const (
	connected outcome = "connected"
	timedOut  outcome = "timed out"
	refused   outcome = "refused"
)

// dialFrom attempts a TCP connection from a local address and classifies
// the result: a drop shows as a timeout, an accept with no listener as a
// refusal, an accept with a listener as a connection.
func dialFrom(local string, port int) outcome {
	d := net.Dialer{Timeout: 2 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(local)}}
	c, err := d.Dial("tcp", net.JoinHostPort(targetIP, fmt.Sprint(port)))
	if err == nil {
		_, _ = c.Write([]byte("hello\n"))
		_ = c.Close()
		return connected
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return refused
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return timedOut
	}
	return outcome("error: " + err.Error())
}

// holdOpen opens a connection from a local address and leaves it open,
// having sent a line so the connection carries traffic.
func holdOpen(t *testing.T, local string, port int) net.Conn {
	t.Helper()
	d := net.Dialer{Timeout: 2 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(local)}}
	c, err := d.Dial("tcp", net.JoinHostPort(targetIP, fmt.Sprint(port)))
	if err != nil {
		t.Fatalf("holding a connection from %s: %v", local, err)
	}
	if _, err := c.Write([]byte("held\n")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func expectDial(t *testing.T, local string, port int, want outcome) {
	t.Helper()
	if got := dialFrom(local, port); got != want {
		t.Fatalf("dial %s -> :%d = %s, want %s", local, port, got, want)
	}
}

// namespace builds a fresh target namespace joined to this one by the
// veth pair, with every peer address on our end, and returns the state
// directory the target's runs share and a way to run a command inside
// the namespace. It skips the test without root, nft, or nsenter.
func namespace(t *testing.T) (stateDir string, inNamespace func(args ...string) *exec.Cmd) {
	t.Helper()
	if os.Getenv(envRole) != "" {
		t.Skip("helper process")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	for _, bin := range []string{"nft", "nsenter"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	stateDir = t.TempDir()

	// A holder process keeps the target namespace alive across phases.
	holder := exec.Command(os.Args[0], "-test.run=^TestNetnsRole$") //nolint:gosec // the test runs itself
	holder.Env = append(os.Environ(), envRole+"=hold")
	holder.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNET}
	holderIn, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holderIn.Close(); _ = holder.Process.Kill(); _ = holder.Wait() })
	nsPath := fmt.Sprintf("/proc/%d/ns/net", holder.Process.Pid)

	// The veth pair: one end moves into the namespace, the other is ours.
	if old, err := netlink.LinkByName(vethPeer); err == nil {
		_ = netlink.LinkDel(old)
	}
	veth := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: vethPeer}, PeerName: vethTarget}
	if err := netlink.LinkAdd(veth); err != nil {
		t.Fatalf("creating veth pair: %v", err)
	}
	t.Cleanup(func() {
		if l, err := netlink.LinkByName(vethPeer); err == nil {
			_ = netlink.LinkDel(l)
		}
	})
	target, err := netlink.LinkByName(vethTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := netlink.LinkSetNsPid(target, holder.Process.Pid); err != nil {
		t.Fatalf("moving %s into the namespace: %v", vethTarget, err)
	}
	peer, err := netlink.LinkByName(vethPeer)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{peerIP + "/24", peer2IP + "/24", heldIP + "/24", heldRemovedIP + "/24"} {
		addr, _ := netlink.ParseAddr(a)
		if err := netlink.AddrAdd(peer, addr); err != nil {
			t.Fatal(err)
		}
	}
	if err := netlink.LinkSetUp(peer); err != nil {
		t.Fatal(err)
	}

	return stateDir, func(args ...string) *exec.Cmd {
		cmd := exec.Command("nsenter", append([]string{"--net=" + nsPath, "--"}, args...)...) //nolint:gosec // the test runs itself inside the namespace
		cmd.Env = append(os.Environ(), envStateDir+"="+stateDir)
		return cmd
	}
}

// TestEnforcementInNamespace is the parent: it builds the topology and
// drives the phases.
func TestEnforcementInNamespace(t *testing.T) {
	stateDir, inNamespace := namespace(t)
	nftList := func() string {
		out, _ := inNamespace("nft", "list", "table", "inet", tableName).CombinedOutput()
		return string(out)
	}

	// Phase 1: the target applies, observes, and changes policy while we
	// send traffic between its READY lines.
	child := newChild(t, inNamespace(os.Args[0], "-test.run=^TestNetnsRole$", "-test.v"), "target")
	child.expectReady("enforced")
	expectDial(t, peerIP, portAllowed, connected)
	expectDial(t, peerIP, portDenied, timedOut)
	expectDial(t, peer2IP, portAllowed, timedOut)
	child.proceed()

	child.expectReady("delta")
	expectDial(t, peer2IP, portAllowed, connected)
	child.proceed()

	// Connections held open while the target adds a rule that sorts
	// before theirs and then removes theirs: the kernel keeps each
	// connection's mark, and the agent must keep naming the rule that
	// admitted it.
	child.expectReady("hold")
	held := holdOpen(t, heldIP, portAllowed)
	heldRemoved := holdOpen(t, heldRemovedIP, portAllowed)
	child.proceed()
	child.expectReady("held")
	_ = held.Close()
	_ = heldRemoved.Close()
	child.proceed()

	child.expectReady("simulation")
	expectDial(t, peerIP, portDenied, connected)
	expectDial(t, peerIP, portAllowed, connected)
	child.proceed()

	child.expectReady("final")
	expectDial(t, peerIP, portDenied, timedOut)
	child.proceed()
	child.wait()

	// The daemon is gone; enforcement is not. The allowed port has no
	// listener now, so an accepted connection is refused; a denied one is
	// still dropped.
	if out := nftList(); !strings.Contains(out, "innerwall terminal enforced") {
		t.Fatalf("table after the daemon exited:\n%s", out)
	}
	expectDial(t, peerIP, portAllowed, refused)
	expectDial(t, peerIP, portDenied, timedOut)

	// A reboot, as far as the kernel is concerned: the table is gone and
	// nothing is dropped.
	if out, err := inNamespace("nft", "delete", "table", "inet", tableName).CombinedOutput(); err != nil {
		t.Fatalf("deleting table: %v: %s", err, out)
	}
	expectDial(t, peerIP, portDenied, refused)

	// Restart re-applies from disk before anything else happens.
	restart := newChild(t, inNamespace(os.Args[0], "-test.run=^TestNetnsRole$", "-test.v"), "restart")
	restart.wait()
	if out := nftList(); !strings.Contains(out, "innerwall terminal enforced") {
		t.Fatalf("table after restart:\n%s", out)
	}
	expectDial(t, peerIP, portDenied, timedOut)

	// The kill switch removes the table and only the table; the persisted
	// policy stays.
	down := newChild(t, inNamespace(os.Args[0], "-test.run=^TestNetnsRole$", "-test.v"), "down")
	down.wait()
	if out := nftList(); !strings.Contains(out, "No such file or directory") && strings.Contains(out, "chain") {
		t.Fatalf("table after teardown:\n%s", out)
	}
	expectDial(t, peerIP, portDenied, refused)
	if _, err := os.Stat(enforce.PolicyPath(stateDir)); err != nil {
		t.Fatalf("teardown removed the persisted policy: %v", err)
	}
	// The other table on the host is untouched by every apply and by the
	// teardown.
	if out, err := inNamespace("nft", "list", "table", "inet", foreignTable).CombinedOutput(); err != nil || !strings.Contains(string(out), "premark") {
		t.Fatalf("foreign table after teardown: %v\n%s", err, out)
	}
}

// TestVisibilityObservationInNamespace is the parent of the visibility
// test: a fresh namespace in which nothing but the agent's owned table
// ever references connection tracking. The target starts its collector,
// then applies a visibility policy, as a newly enrolled agent does with
// its first snapshot; the peer connects; and the target must see the
// connections arrive in a closed flow window, without restarting
// anything. Nothing is dropped, on any port, from any peer.
func TestVisibilityObservationInNamespace(t *testing.T) {
	_, inNamespace := namespace(t)
	child := newChild(t, inNamespace(os.Args[0], "-test.run=^TestNetnsRole$", "-test.v"), "visibility")
	child.expectReady("visibility")
	expectDial(t, peerIP, portAllowed, connected)
	expectDial(t, peer2IP, portDenied, connected)
	child.proceed()
	child.wait()
}

// child drives one in-namespace run of this binary.
type child struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin io.WriteCloser
	ready chan string
	done  chan error
}

func newChild(t *testing.T, cmd *exec.Cmd, role string) *child {
	t.Helper()
	cmd.Env = append(cmd.Env, envRole+"="+role)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &child{t: t, cmd: cmd, stdin: stdin, ready: make(chan string, 16), done: make(chan error, 1)}
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			t.Logf("[%s] %s", role, line)
			if rest, ok := strings.CutPrefix(line, "READY "); ok {
				c.ready <- rest
			}
		}
	}()
	go func() { c.done <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return c
}

func (c *child) expectReady(phase string) {
	c.t.Helper()
	select {
	case got := <-c.ready:
		if got != phase {
			c.t.Fatalf("child reported READY %s, want %s", got, phase)
		}
	case err := <-c.done:
		c.t.Fatalf("child exited before READY %s: %v", phase, err)
	case <-time.After(30 * time.Second):
		c.t.Fatalf("timed out waiting for READY %s", phase)
	}
}

// proceed tells the child the traffic for its current phase is done.
func (c *child) proceed() {
	c.t.Helper()
	if _, err := io.WriteString(c.stdin, "go\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *child) wait() {
	c.t.Helper()
	_ = c.stdin.Close()
	select {
	case err := <-c.done:
		if err != nil {
			c.t.Fatalf("child failed: %v", err)
		}
	case <-time.After(60 * time.Second):
		c.t.Fatal("timed out waiting for the child to exit")
	}
}

// TestNetnsRole is the in-namespace side, selected by INNERWALL_NETNS_ROLE.
func TestNetnsRole(t *testing.T) {
	switch os.Getenv(envRole) {
	case "":
		t.Skip("run by TestEnforcementInNamespace")
	case "hold":
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "target":
		runTarget(t)
	case "visibility":
		runVisibility(t)
	case "restart":
		s := nft.New(nft.Config{StateDir: os.Getenv(envStateDir), Table: tableName, NflogGroup: nflogGroup})
		if err := s.Load(); err != nil {
			t.Fatal(err)
		}
		if s.Current().GetVersion() != 7 || s.Mode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
			t.Fatalf("loaded policy = %v", s.Current())
		}
		if err := s.Restore(context.Background()); err != nil {
			t.Fatal(err)
		}
		if s.LastApply() != nft.ApplyFull {
			t.Fatalf("restore kind = %s", s.LastApply())
		}
	case "down":
		if err := nft.Teardown(context.Background(), nil, nft.Options{Table: tableName}); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown role %q", os.Getenv(envRole))
	}
}

type observed struct {
	mu  sync.Mutex
	obs []collect.Observation
}

func (o *observed) add(ob collect.Observation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.obs = append(o.obs, ob)
}

// waitFor polls for an observation matching cond. Polling is a test
// device only.
func (o *observed) waitFor(t *testing.T, what string, cond func(collect.Observation) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		o.mu.Lock()
		for _, ob := range o.obs {
			if cond(ob) {
				o.mu.Unlock()
				return
			}
		}
		snapshot := append([]collect.Observation(nil), o.obs...)
		o.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no observation for %s; saw %+v", what, snapshot)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (o *observed) none(t *testing.T, what string, cond func(collect.Observation) bool) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, ob := range o.obs {
		if cond(ob) {
			t.Fatalf("unexpected observation for %s: %+v", what, ob)
		}
	}
}

// rawMarks records every connection mark the kernel reported.
type rawMarks struct {
	mu   sync.Mutex
	seen []uint32
}

func (r *rawMarks) add(mark uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, mark)
}

// expect waits for a mark exactly equal to want. Polling is a test device
// only.
func (r *rawMarks) expect(t *testing.T, what string, want uint32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.mu.Lock()
		for _, m := range r.seen {
			if m == want {
				r.mu.Unlock()
				return
			}
		}
		snapshot := append([]uint32(nil), r.seen...)
		r.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no mark %#x for %s; saw %#x", want, what, snapshot)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// noneWithout fails if any mark the agent set has lost the foreign bits.
func (r *rawMarks) noneWithout(t *testing.T, foreign uint32) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.seen {
		if nft.RuleIndex(m) != 0 && m&nft.ForeignMask != foreign {
			t.Fatalf("mark %#x carries our region but lost the foreign bits %#x", m, foreign)
		}
	}
}

// liveMark dumps the namespace's connection table and returns the mark of
// the tracked connection from src to the allowed port. Polling is a test
// device only.
func liveMark(t *testing.T, src string) uint32 {
	t.Helper()
	c, err := ct.Dial(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	want := netip.MustParseAddr(src)
	deadline := time.Now().Add(10 * time.Second)
	for {
		flows, err := c.Dump(nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range flows {
			if f.TupleOrig.IP.SourceAddress.Unmap() == want && f.TupleOrig.Proto.DestinationPort == portAllowed {
				return f.Mark
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tracked connection from %s", src)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func policyAt(version uint64, mode innerwallv1.EnforcementMode, peers ...string) *innerwallv1.WorkloadPolicy {
	return &innerwallv1.WorkloadPolicy{Version: version, Mode: mode, InboundRules: []*innerwallv1.ResolvedRule{
		{RuleId: ruleA, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: peers, Ports: []*innerwallv1.PortRange{{Start: portAllowed, End: portAllowed}}},
	}}
}

func runTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdin := bufio.NewScanner(os.Stdin)
	waitGo := func() {
		t.Helper()
		if !stdin.Scan() {
			t.Fatal("parent closed stdin")
		}
	}

	upTarget(t)

	// Another user of the connection mark, installed before the agent
	// and never touched by it.
	if err := (nft.ExecRunner{}).Apply(ctx, foreignScript); err != nil {
		t.Fatalf("installing the foreign table: %v", err)
	}

	// The agent side: the store over real nft, and the two sources. The
	// classifier is wrapped to record every raw mark the kernel reports,
	// so the test can see the foreign bits beside ours.
	store := nft.New(nft.Config{StateDir: os.Getenv(envStateDir), Table: tableName, NflogGroup: nflogGroup})
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	obs := &observed{}
	marks := &rawMarks{}
	sources := collect.Sources{
		&conntrack.Source{Classify: func(mark uint32) (innerwallv1.PolicyDecision, string) {
			marks.add(mark)
			return store.Classify(mark)
		}},
		&nflog.Source{Group: nflogGroup, Decide: store.TerminalDecision},
	}
	go func() { _ = sources.Run(ctx, obs.add) }()
	time.Sleep(300 * time.Millisecond) // let both subscriptions register

	from := func(ip string, port uint16, d innerwallv1.PolicyDecision) func(collect.Observation) bool {
		return func(o collect.Observation) bool {
			return o.Src == netip.MustParseAddr(ip) && o.DstPort == port && o.Decision == d && o.Protocol == innerwallv1.Protocol_PROTOCOL_TCP
		}
	}

	// Enforced: peer may reach the allowed port and nothing else.
	if err := store.Apply(ctx, policyAt(1, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, peerIP+"/32")); err != nil {
		t.Fatal(err)
	}
	if store.LastApply() != nft.ApplyFull {
		t.Fatalf("first apply kind = %s", store.LastApply())
	}
	fmt.Println("READY enforced")
	waitGo()
	obs.waitFor(t, "allowed connection attributed to rule A", func(o collect.Observation) bool {
		return from(peerIP, portAllowed, innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED)(o) && o.RuleID == ruleA && o.Connections == 1
	})
	obs.waitFor(t, "blocked attempt on the denied port", func(o collect.Observation) bool {
		return from(peerIP, portDenied, innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED)(o) && o.Connections == 1 && o.Bytes > 0
	})
	obs.waitFor(t, "blocked attempt from the unlisted peer", from(peer2IP, portAllowed, innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED))
	obs.none(t, "the denied port allowed", from(peerIP, portDenied, innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED))
	// The accepted connection carries our rule number in the region and
	// the foreign mark, untouched, in the low bits.
	marks.expect(t, "rule 1 beside the foreign mark", nft.RuleMark(1)|foreignMark)

	// Delta: the second peer address joins rule A's set; no full reload.
	if err := store.Apply(ctx, policyAt(2, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, peerIP+"/32", peer2IP+"/32")); err != nil {
		t.Fatal(err)
	}
	if store.LastApply() != nft.ApplyDelta {
		t.Fatalf("peer-only change applied as %s", store.LastApply())
	}
	fmt.Println("READY delta")
	waitGo()
	obs.waitFor(t, "second peer allowed after the set update", func(o collect.Observation) bool {
		return from(peer2IP, portAllowed, innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED)(o) && o.RuleID == ruleA
	})

	// Held connections: two peers join rule A and hold a connection open
	// each, marked with rule A's value.
	held := policyAt(3, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, peerIP+"/32", peer2IP+"/32", heldIP+"/32", heldRemovedIP+"/32")
	if err := store.Apply(ctx, held); err != nil {
		t.Fatal(err)
	}
	fmt.Println("READY hold")
	waitGo()
	markA, ok := store.Mark(ruleA)
	if !ok {
		t.Fatal("rule A has no value")
	}
	for _, src := range []string{heldIP, heldRemovedIP} {
		if got := liveMark(t, src); nft.RuleIndex(got) != nft.RuleIndex(markA) {
			t.Fatalf("held connection from %s carries %#x, want rule A's %#x", src, got, markA)
		}
	}
	// A rule that sorts before rule A is added. Rule A keeps its value, and
	// the held connection, marked under the earlier version, still names
	// rule A, not the newcomer.
	early := &innerwallv1.ResolvedRule{RuleId: ruleEarly, Protocol: innerwallv1.Protocol_PROTOCOL_TCP, PeerCidrs: []string{"10.99.0.200/32"}, Ports: []*innerwallv1.PortRange{{Start: portDenied, End: portDenied}}}
	renumbered := policyAt(4, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, peerIP+"/32", peer2IP+"/32", heldIP+"/32", heldRemovedIP+"/32")
	renumbered.InboundRules = append(renumbered.InboundRules, early)
	if err := store.Apply(ctx, renumbered); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Mark(ruleEarly); m == markA {
		t.Fatal("the new rule took rule A's value")
	}
	if d, id := store.Classify(liveMark(t, heldIP)); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != ruleA {
		t.Fatalf("held connection after a rule was added before its rule = %v %s, want rule A", d, id)
	}
	// Rule A is removed. The established connection survives, still
	// marked, and names the removed rule A, never the rule left in place.
	removed := &innerwallv1.WorkloadPolicy{Version: 5, Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, InboundRules: []*innerwallv1.ResolvedRule{early}}
	if err := store.Apply(ctx, removed); err != nil {
		t.Fatal(err)
	}
	if d, id := store.Classify(liveMark(t, heldRemovedIP)); d != innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED || id != ruleA {
		t.Fatalf("held connection after its rule was removed = %v %s, want the removed rule A", d, id)
	}
	fmt.Println("READY held")
	waitGo()

	// Simulation: everything passes; what enforcement would drop is
	// reported as WOULD_BLOCK. Rule A returns on its own value.
	if err := store.Apply(ctx, policyAt(6, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, peerIP+"/32", peer2IP+"/32")); err != nil {
		t.Fatal(err)
	}
	if store.LastApply() != nft.ApplyFull {
		t.Fatalf("mode change applied as %s", store.LastApply())
	}
	fmt.Println("READY simulation")
	waitGo()
	obs.waitFor(t, "would-block on the denied port", func(o collect.Observation) bool {
		return from(peerIP, portDenied, innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK)(o) && o.Connections == 1
	})
	// The would-block connection is marked the same way: region set, foreign
	// bits kept.
	marks.expect(t, "would-block beside the foreign mark", nft.WouldBlockMark|foreignMark)
	marks.noneWithout(t, foreignMark)
	obs.waitFor(t, "allowed port still attributed under simulation", func(o collect.Observation) bool {
		return from(peerIP, portAllowed, innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED)(o) && o.RuleID == ruleA
	})

	// Back to enforced, then exit with the rules in place.
	if m, _ := store.Mark(ruleA); m != markA {
		t.Fatalf("rule A returned on %#x, want its own %#x", m, markA)
	}
	if err := store.Apply(ctx, policyAt(7, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, peerIP+"/32", peer2IP+"/32")); err != nil {
		t.Fatal(err)
	}
	listing, err := store.List(ctx)
	if err != nil || !strings.Contains(listing, "innerwall terminal enforced") || !strings.Contains(listing, nft.SetName(ruleA, false)) {
		t.Fatalf("listing = %v\n%s", err, listing)
	}
	fmt.Println("READY final")
	waitGo()
	obs.waitFor(t, "blocked again after returning to enforced", from(peerIP, portDenied, innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED))
}

// upTarget brings the target namespace up: loopback, our end of the veth
// pair with the target address, and a listener on each port that reads
// whatever it is sent. The listeners close when the test ends.
func upTarget(t *testing.T) {
	t.Helper()
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	_ = netlink.LinkSetUp(lo)
	var link netlink.Link
	for deadline := time.Now().Add(5 * time.Second); ; {
		link, err = netlink.LinkByName(vethTarget)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared: %v", vethTarget, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	addr, _ := netlink.ParseAddr(targetIP + "/24")
	if err := netlink.AddrAdd(link, addr); err != nil {
		t.Fatal(err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{portAllowed, portDenied} {
		l, err := net.Listen("tcp", net.JoinHostPort(targetIP, fmt.Sprint(port)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				go func() { _, _ = io.Copy(io.Discard, c); _ = c.Close() }()
			}
		}()
	}
}

// runVisibility is the target of the visibility test. Nothing in this
// namespace references connection tracking before the agent applies, and
// nothing but the owned table ever does: no foreign table, no scratch
// rule. The collector, sources, aggregation, and buffer are the daemon's,
// on a one-second window, and they start before the policy is applied, as
// they do on a newly enrolled host, so the test also shows that sources
// subscribed before connection tracking was engaged need no restart.
func runVisibility(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdin := bufio.NewScanner(os.Stdin)

	upTarget(t)

	store := nft.New(nft.Config{StateDir: os.Getenv(envStateDir), Table: tableName, NflogGroup: nflogGroup})
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if store.Current() != nil {
		t.Fatalf("a fresh state directory loaded %v", store.Current())
	}
	buffer := collect.NewBuffer(0)
	collector := &collect.Collector{
		Source: collect.Sources{
			&conntrack.Source{Classify: store.Classify},
			&nflog.Source{Group: nflogGroup, Decide: store.TerminalDecision},
		},
		Buffer: buffer,
		Gaps:   &collect.Gaps{},
	}
	collector.SetConfig(&innerwallv1.SyncConfig{FlowAggregationWindowSeconds: 1})
	go func() { _ = collector.Run(ctx) }()
	time.Sleep(300 * time.Millisecond) // let both subscriptions register

	// The first snapshot: visibility, carrying a rule that, enforced,
	// would admit only the first peer to the allowed port. In visibility
	// it renders nothing.
	if err := store.Apply(ctx, policyAt(1, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, peerIP+"/32")); err != nil {
		t.Fatal(err)
	}
	listing, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listing, "chain "+nft.ObserveChainName+" {") || !strings.Contains(listing, "ct state new") ||
		strings.Count(listing, "chain ") != 1 || strings.Contains(listing, "set ") ||
		strings.Contains(listing, "drop") || strings.Contains(listing, "log ") || strings.Contains(listing, "mark") {
		t.Fatalf("visibility table is not exactly the observation chain:\n%s", listing)
	}
	fmt.Println("READY visibility")
	if !stdin.Scan() {
		t.Fatal("parent closed stdin")
	}

	// Both connections, the one the rule names and the one it does not,
	// arrive in a closed window as OBSERVED, and nothing else does.
	want := map[string]bool{
		fmt.Sprintf("%s:%d", peerIP, portAllowed): false,
		fmt.Sprintf("%s:%d", peer2IP, portDenied): false,
	}
	wait, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	var seen []*innerwallv1.FlowRecord
	for missing := len(want); missing > 0; {
		w, ok := buffer.Pop(wait)
		if !ok {
			t.Fatalf("no flow window carried every connection within 15s; records seen: %v", seen)
		}
		for _, r := range w.Records {
			seen = append(seen, r)
			if r.GetDecision() != innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED || r.GetMatchedRuleId() != "" {
				t.Fatalf("visibility reported a decision: %v", r)
			}
			key := fmt.Sprintf("%s:%d", r.GetSrcAddress(), r.GetDstPort())
			if done, ok := want[key]; ok && !done && r.GetConnectionCount() > 0 && r.GetProtocol() == innerwallv1.Protocol_PROTOCOL_TCP {
				want[key] = true
				missing--
			}
		}
	}
}
