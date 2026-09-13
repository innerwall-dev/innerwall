package storetest

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/ingest"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store"
)

// Fleet is the seeded estate SeedFleet builds: three workloads in three
// states, one address group, one ruleset rendered onto them, and two
// reporting windows of flows that produce recognizable screen states (a
// populated rollup with would-block traffic, a degraded workload, an
// offline one). Every instant is relative to Now, a fixed clock, so a
// test that reads the seed sets its own clock to Now.
//
//   - web-1 (role=web, env=prod): enforced, synced, healthy. Its only
//     inbound traffic is blocked: no rule admits it.
//   - db-1 (role=db, env=prod): simulation, degraded (the agent refused an
//     apply), renewal failing, dropping flow records. Admits web-1 on
//     tcp/5432 by the ruleset; sees would-block traffic from the office
//     group, an unknown address, and web-1 on tcp/22.
//   - cache-1 (role=cache, env=prod): visibility, offline for three
//     hours, credential expired. Observes web-1 on tcp/6379.
type Fleet struct {
	Now   time.Time
	Token enroll.Token
	Web   identity.WorkloadID
	DB    identity.WorkloadID
	Cache identity.WorkloadID
	// Addresses of the three workloads.
	WebAddr, DBAddr, CacheAddr netip.Addr
	Office                     policy.AddressGroup
	Ruleset                    policy.Ruleset
	// DBRuleID is the rendered rule on db-1 admitting web-1 on tcp/5432.
	DBRuleID string
	// DBVersion is db-1's latest rendered version.
	DBVersion uint64
	// Window1 and Window2 are the two reporting windows, five minutes
	// each, starting two hours and one hour before Now.
	Window1, Window2 time.Time
	// WindowLength is the length of each window.
	WindowLength time.Duration
}

// SeedFleet populates s with the fleet described on Fleet. The store must
// be empty.
func SeedFleet(t *testing.T, s *store.Store) *Fleet {
	t.Helper()
	ctx := context.Background()
	f := &Fleet{Now: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC), WindowLength: 5 * time.Minute}
	f.Window1, f.Window2 = f.Now.Add(-2*time.Hour), f.Now.Add(-time.Hour)
	f.WebAddr, f.DBAddr, f.CacheAddr = netip.MustParseAddr("10.0.0.10"), netip.MustParseAddr("10.0.0.20"), netip.MustParseAddr("10.0.0.30")

	_, hash, err := enroll.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	f.Token = enroll.Token{ID: uuid.New(), Hash: hash, Name: "prod", Labels: []enroll.Label{{Key: "env", Value: "prod"}}, CreatedAt: f.Now.Add(-48 * time.Hour), ExpiresAt: f.Now.Add(28 * 24 * time.Hour)}
	if err := s.CreateToken(ctx, f.Token); err != nil {
		t.Fatal(err)
	}

	enrollOne := func(hostname, role string, expires time.Time, addr netip.Addr, enrolledAt time.Time) identity.WorkloadID {
		id, err := identity.NewWorkloadID()
		if err != nil {
			t.Fatal(err)
		}
		w := enroll.Workload{ID: id, TokenID: f.Token.ID, Hostname: hostname, Labels: []enroll.Label{{Key: "env", Value: "prod"}, {Key: "role", Value: role}}, EnrolledAt: enrolledAt, CredentialSerial: hostname + "-1", CredentialExpiresAt: expires}
		if err := s.CreateWorkload(ctx, w, enrolledAt); err != nil {
			t.Fatal(err)
		}
		facts := &innerwallv1.HostFacts{Hostname: hostname, Os: &innerwallv1.OsInfo{Family: "linux", Name: "debian", Version: "13", KernelVersion: "6.12", Architecture: "amd64"}, Interfaces: []*innerwallv1.NetworkInterface{{Name: "eth0", Addresses: []string{addr.String() + "/24"}}}}
		if _, err := s.RecordFacts(ctx, id, facts, enrolledAt); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.Web = enrollOne("web-1", "web", f.Now.Add(20*time.Hour), f.WebAddr, f.Now.Add(-36*time.Hour))
	f.DB = enrollOne("db-1", "db", f.Now.Add(2*time.Hour), f.DBAddr, f.Now.Add(-30*time.Hour))
	f.Cache = enrollOne("cache-1", "cache", f.Now.Add(-time.Hour), f.CacheAddr, f.Now.Add(-24*time.Hour))
	if err := s.RecordListeningServices(ctx, f.DB, []registry.ListeningService{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Port: 5432, ProcessName: "postgres", ProcessPath: "/usr/lib/postgresql/16/bin/postgres"}}, f.Now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	engine := &compiler.Engine{Store: s}
	authoring := &policy.Authoring{Store: s, Renderer: renderer{engine}, Now: func() time.Time { return f.Now.Add(-12 * time.Hour) }}
	f.Office = policy.AddressGroup{Name: "office", CIDRs: []string{"192.0.2.0/24"}}
	if err := authoring.CreateAddressGroup(ctx, &f.Office); err != nil {
		t.Fatal(err)
	}
	f.Ruleset = policy.Ruleset{
		Name: "web-to-db", Description: "web tier reaches the database", Enabled: true,
		Scope: policy.Selector{"role": {"db"}},
		Rules: []policy.Rule{{
			Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Description: "postgres from web",
			Peers:   []policy.Peer{{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web"}}}},
			Entries: []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}}}},
		}},
	}
	if err := authoring.CreateRuleset(ctx, &f.Ruleset); err != nil {
		t.Fatal(err)
	}
	for id, mode := range map[identity.WorkloadID]innerwallv1.EnforcementMode{
		f.Web:   innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED,
		f.DB:    innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION,
		f.Cache: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY,
	} {
		if err := s.SetWorkloadMode(ctx, id, mode); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engine.Render(ctx); err != nil {
		t.Fatal(err)
	}
	dbPolicy, err := s.GetWorkloadPolicy(ctx, f.DB)
	if err != nil {
		t.Fatal(err)
	}
	if dbPolicy == nil || len(dbPolicy.GetInboundRules()) != 1 {
		t.Fatalf("db-1 rendered policy = %v, want one rule", dbPolicy)
	}
	f.DBRuleID, f.DBVersion = dbPolicy.GetInboundRules()[0].GetRuleId(), dbPolicy.GetVersion()
	webPolicy, err := s.GetWorkloadPolicy(ctx, f.Web)
	if err != nil {
		t.Fatal(err)
	}

	// Sync and health, as the gateway would have recorded them.
	if err := s.RecordAgent(ctx, f.Web, registry.AgentInfo{Version: "0.3.0", Capabilities: []string{"nftables", "conntrack"}}, webPolicy.GetVersion(), f.Now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplied(ctx, f.Web, webPolicy.GetVersion(), innerwallv1.SyncState_SYNC_STATE_SYNCED, f.Now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHeartbeat(ctx, f.Web, 0, "", f.Now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAgent(ctx, f.DB, registry.AgentInfo{Version: "0.3.0", Capabilities: []string{"nftables", "conntrack"}}, 0, f.Now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSyncState(ctx, f.DB, innerwallv1.SyncState_SYNC_STATE_DEGRADED, "apply refused: set element exceeds the table's size", f.Now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHeartbeat(ctx, f.DB, 42, "renewal refused: authority unreachable", f.Now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAgent(ctx, f.Cache, registry.AgentInfo{Version: "0.2.0"}, 0, f.Now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSyncState(ctx, f.Cache, innerwallv1.SyncState_SYNC_STATE_OFFLINE, "", f.Now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Flows, resolved at ingest against the registry as it now stands.
	workloads, err := s.ListWorkloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := s.ListAddressGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	index := ingest.BuildIndex(workloads, groups)
	record := func(src string, dst netip.Addr, port uint16, decision innerwallv1.PolicyDecision, rule string, conns, bytes uint64, start time.Time) flowstore.Record {
		addr := netip.MustParseAddr(src)
		return flowstore.Record{
			Peer: index.Resolve(addr), SrcAddress: addr, DstAddress: dst, DstPort: port,
			Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Direction: innerwallv1.Direction_DIRECTION_INBOUND, Decision: decision,
			MatchedRuleID: rule, ConnectionCount: conns, ByteCount: bytes, FirstSeen: start.Add(10 * time.Second), LastSeen: start.Add(f.WindowLength - 10*time.Second),
		}
	}
	const (
		allowed    = innerwallv1.PolicyDecision_POLICY_DECISION_ALLOWED
		wouldBlock = innerwallv1.PolicyDecision_POLICY_DECISION_WOULD_BLOCK
		blocked    = innerwallv1.PolicyDecision_POLICY_DECISION_BLOCKED
		observed   = innerwallv1.PolicyDecision_POLICY_DECISION_OBSERVED
	)
	for _, start := range []time.Time{f.Window1, f.Window2} {
		windows := []flowstore.Window{
			{WorkloadID: f.DB, Start: start, End: start.Add(f.WindowLength), Records: []flowstore.Record{
				record(f.WebAddr.String(), f.DBAddr, 5432, allowed, f.DBRuleID, 120, 480_000, start),
				record("192.0.2.7", f.DBAddr, 5432, wouldBlock, "", 3, 900, start),
				record("198.51.100.7", f.DBAddr, 22, wouldBlock, "", 9, 2700, start),
				record(f.WebAddr.String(), f.DBAddr, 22, wouldBlock, "", 1, 300, start),
			}},
			{WorkloadID: f.Web, Start: start, End: start.Add(f.WindowLength), Records: []flowstore.Record{
				record("203.0.113.9", f.WebAddr, 443, blocked, "", 50, 25_000, start),
				record("192.0.2.8", f.WebAddr, 443, blocked, "", 8, 4_000, start),
			}},
			{WorkloadID: f.Cache, Start: start, End: start.Add(f.WindowLength), Records: []flowstore.Record{
				record(f.WebAddr.String(), f.CacheAddr, 6379, observed, "", 200, 100_000, start),
			}},
		}
		for _, w := range windows {
			if _, err := s.Flows().WriteWindow(ctx, w); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f
}

// renderer adapts the engine to the authoring service's Renderer.
type renderer struct{ engine *compiler.Engine }

func (r renderer) Render(ctx context.Context) error {
	_, err := r.engine.Render(ctx)
	return err
}
