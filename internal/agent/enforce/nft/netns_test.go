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
// One test binary plays every part. The parent test creates the
// namespace (a holder process started with a new network namespace keeps
// it alive), builds the veth pair, and runs itself again inside the
// namespace for each phase, driving it over stdin/stdout.

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

	portAllowed = 18080
	portDenied  = 19090

	ruleA = "0191e5c0-0000-7000-8000-00000000000a/tcp"

	nflogGroup = 201
	tableName  = "innerwalltest"
)

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

func expectDial(t *testing.T, local string, port int, want outcome) {
	t.Helper()
	if got := dialFrom(local, port); got != want {
		t.Fatalf("dial %s -> :%d = %s, want %s", local, port, got, want)
	}
}

// TestEnforcementInNamespace is the parent: it builds the topology and
// drives the phases.
func TestEnforcementInNamespace(t *testing.T) {
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
	stateDir := t.TempDir()

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
	for _, a := range []string{peerIP + "/24", peer2IP + "/24"} {
		addr, _ := netlink.ParseAddr(a)
		if err := netlink.AddrAdd(peer, addr); err != nil {
			t.Fatal(err)
		}
	}
	if err := netlink.LinkSetUp(peer); err != nil {
		t.Fatal(err)
	}

	inNamespace := func(args ...string) *exec.Cmd {
		cmd := exec.Command("nsenter", append([]string{"--net=" + nsPath, "--"}, args...)...) //nolint:gosec // the test runs itself inside the namespace
		cmd.Env = append(os.Environ(), envStateDir+"="+stateDir)
		return cmd
	}
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
	case "restart":
		s := nft.New(nft.Config{StateDir: os.Getenv(envStateDir), Table: tableName, NflogGroup: nflogGroup})
		if err := s.Load(); err != nil {
			t.Fatal(err)
		}
		if s.Current().GetVersion() != 4 || s.Mode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
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

	// Bring the namespace up: loopback and our end of the veth pair.
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
		defer func() { _ = l.Close() }()
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

	// The agent side: the store over real nft, and the two sources.
	store := nft.New(nft.Config{StateDir: os.Getenv(envStateDir), Table: tableName, NflogGroup: nflogGroup})
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	obs := &observed{}
	sources := collect.Sources{
		&conntrack.Source{Classify: store.Classify},
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

	// Simulation: everything passes; what enforcement would drop is
	// reported as WOULD_BLOCK.
	if err := store.Apply(ctx, policyAt(3, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, peerIP+"/32", peer2IP+"/32")); err != nil {
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
	obs.waitFor(t, "allowed port still attributed under simulation", func(o collect.Observation) bool {
		return from(peerIP, portAllowed, innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED)(o) && o.RuleID == ruleA
	})

	// Back to enforced, then exit with the rules in place.
	if err := store.Apply(ctx, policyAt(4, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, peerIP+"/32", peer2IP+"/32")); err != nil {
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
