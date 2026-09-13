package store_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/rendered"
	"github.com/innerwall-dev/innerwall/internal/store"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// enrollWorkload persists a token and a workload the way enrollment does.
func enrollWorkload(t *testing.T, s *store.Store, labels ...enroll.Label) identity.WorkloadID {
	t.Helper()
	ctx := context.Background()
	_, hash, _ := enroll.NewToken()
	tok := enroll.Token{ID: uuid.New(), Hash: hash, Name: "t", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	id, _ := identity.NewWorkloadID()
	w := enroll.Workload{ID: id, TokenID: tok.ID, Hostname: "h", Labels: labels, EnrolledAt: time.Now(), CredentialSerial: "1", CredentialExpiresAt: time.Now().Add(time.Hour)}
	if err := s.CreateWorkload(ctx, w, time.Now()); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAuthoredModelRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	now := time.Now()

	svc := &policy.Service{ID: uuid.New(), Name: "postgres", CreatedAt: now, UpdatedAt: now, Entries: []policy.ServiceEntry{
		{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Ports: []policy.PortRange{{Start: 5432, End: 5432}, {Start: 6000, End: 6010}}},
		{Protocol: innerwallv1.Protocol_PROTOCOL_ICMP},
	}}
	if err := s.CreateService(ctx, svc); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateService(ctx, &policy.Service{ID: uuid.New(), Name: "postgres", Entries: svc.Entries}); !errors.Is(err, policy.ErrDuplicateName) {
		t.Fatalf("duplicate name err = %v", err)
	}
	got, err := s.GetService(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "postgres" || len(got.Entries) != 2 || len(got.Entries[0].Ports) != 2 || got.Entries[0].Ports[1].End != 6010 || got.Entries[1].Protocol != innerwallv1.Protocol_PROTOCOL_ICMP || len(got.Entries[1].Ports) != 0 {
		t.Fatalf("service = %+v", got)
	}

	grp := &policy.AddressGroup{ID: uuid.New(), Name: "corp", CIDRs: []string{"10.0.0.0/8", "fd00::/8"}, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateAddressGroup(ctx, grp); err != nil {
		t.Fatal(err)
	}

	rs := &policy.Ruleset{
		ID: uuid.New(), Name: "to-db", Description: "d", Enabled: true, CreatedAt: now, UpdatedAt: now,
		Scope: policy.Selector{"role": {"db"}, "env": {"prod", "staging"}},
		Rules: []policy.Rule{{
			ID: uuid.New(), Direction: innerwallv1.Direction_DIRECTION_INBOUND, Enabled: true, Description: "r",
			Peers: []policy.Peer{
				{Kind: policy.PeerWorkloads, Workloads: policy.Selector{"role": {"web"}}},
				{Kind: policy.PeerAddressGroup, AddressGroupID: grp.ID},
				{Kind: policy.PeerCIDR, CIDR: "203.0.113.0/24"},
			},
			ServiceIDs: []uuid.UUID{svc.ID},
			Entries:    []policy.ServiceEntry{{Protocol: innerwallv1.Protocol_PROTOCOL_UDP}},
		}},
	}
	if err := s.CreateRuleset(ctx, rs); err != nil {
		t.Fatal(err)
	}
	back, err := s.GetRuleset(ctx, rs.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name != "to-db" || !back.Enabled || len(back.Scope) != 2 || len(back.Scope["env"]) != 2 || len(back.Rules) != 1 {
		t.Fatalf("ruleset = %+v", back)
	}
	r := back.Rules[0]
	if r.ID != rs.Rules[0].ID || len(r.Peers) != 3 || r.Peers[0].Kind != policy.PeerWorkloads || r.Peers[0].Workloads["role"][0] != "web" ||
		r.Peers[1].AddressGroupID != grp.ID || r.Peers[2].CIDR != "203.0.113.0/24" || len(r.ServiceIDs) != 1 || r.ServiceIDs[0] != svc.ID ||
		len(r.Entries) != 1 || r.Entries[0].Protocol != innerwallv1.Protocol_PROTOCOL_UDP {
		t.Fatalf("rule = %+v", r)
	}

	// Referenced objects cannot be deleted; unreferenced ones can.
	if err := s.DeleteService(ctx, svc.ID); !errors.Is(err, policy.ErrInUse) {
		t.Fatalf("delete referenced service err = %v", err)
	}
	if err := s.DeleteAddressGroup(ctx, grp.ID); !errors.Is(err, policy.ErrInUse) {
		t.Fatalf("delete referenced group err = %v", err)
	}
	// Update replaces rules wholesale.
	rs.Rules = nil
	rs.Enabled = false
	if err := s.UpdateRuleset(ctx, rs); err != nil {
		t.Fatal(err)
	}
	back, _ = s.GetRuleset(ctx, rs.ID)
	if back.Enabled || len(back.Rules) != 0 {
		t.Fatalf("updated ruleset = %+v", back)
	}
	if err := s.DeleteService(ctx, svc.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRuleset(ctx, rs.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRuleset(ctx, rs.ID); !errors.Is(err, policy.ErrRulesetUnknown) {
		t.Fatalf("second delete err = %v", err)
	}
	if _, err := s.GetService(ctx, svc.ID); !errors.Is(err, policy.ErrServiceUnknown) {
		t.Fatalf("get deleted err = %v", err)
	}
}

func TestRegistryAndRenderTx(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	id := enrollWorkload(t, s, enroll.Label{Key: "role", Value: "db"})
	now := time.Now().Truncate(time.Microsecond)

	// Inventory: addresses derived and change detection.
	facts := &innerwallv1.HostFacts{Hostname: "db-1", Interfaces: []*innerwallv1.NetworkInterface{{Name: "eth0", Addresses: []string{"10.0.0.10/24", "127.0.0.1/8"}}}}
	changed, err := s.RecordFacts(ctx, id, facts, now)
	if err != nil || !changed {
		t.Fatalf("first facts changed=%v err=%v", changed, err)
	}
	changed, err = s.RecordFacts(ctx, id, facts, now)
	if err != nil || changed {
		t.Fatalf("same facts changed=%v err=%v", changed, err)
	}
	if err := s.RecordListeningServices(ctx, id, []registry.ListeningService{{Protocol: innerwallv1.Protocol_PROTOCOL_TCP, Port: 5432, ProcessName: "postgres"}}, now); err != nil {
		t.Fatal(err)
	}
	if svcs, _ := s.ListListeningServices(ctx, id); len(svcs) != 1 || svcs[0].Port != 5432 || svcs[0].ProcessName != "postgres" {
		t.Fatalf("listening = %+v", svcs)
	}
	if err := s.RecordListeningServices(ctx, id, nil, now); err != nil {
		t.Fatal(err)
	}
	w, err := s.LookupWorkload(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if w.Hostname != "db-1" || len(w.Addresses) != 1 || w.Addresses[0] != netip.MustParseAddr("10.0.0.10") || w.Facts.GetHostname() != "db-1" || w.LastSeenAt == nil || !w.LastSeenAt.Equal(now) {
		t.Fatalf("workload = %+v", w)
	}
	if w.SyncState != innerwallv1.SyncState_SYNC_STATE_OFFLINE || w.Mode != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY {
		t.Fatalf("defaults: state=%v mode=%v", w.SyncState, w.Mode)
	}
	svcs, _ := s.ListListeningServices(ctx, id)
	if len(svcs) != 0 { // second report replaced them with nothing
		t.Fatalf("listening = %+v", svcs)
	}

	// Labels, mode, status.
	if err := s.SetWorkloadLabels(ctx, id, []registry.Label{{Key: "role", Value: "web"}, {Key: "env", Value: "prod"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkloadMode(ctx, id, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "v", Capabilities: []string{"a"}}, 3, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordApplied(ctx, id, 4, innerwallv1.SyncState_SYNC_STATE_SYNCED, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHeartbeat(ctx, id, 7, "", now); err != nil {
		t.Fatal(err)
	}
	w, _ = s.LookupWorkload(ctx, id)
	if len(w.Labels) != 2 || w.Labels[0].Key != "env" || w.Mode != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED || w.Agent.Version != "v" || w.AppliedVersion != 4 || w.SyncState != innerwallv1.SyncState_SYNC_STATE_SYNCED || w.DroppedFlowRecords != 7 {
		t.Fatalf("workload = %+v", w)
	}
	if err := s.SetSyncState(ctx, id, innerwallv1.SyncState_SYNC_STATE_DEGRADED, "boom", now); err != nil {
		t.Fatal(err)
	}
	w, _ = s.LookupWorkload(ctx, id)
	if w.SyncState != innerwallv1.SyncState_SYNC_STATE_DEGRADED || w.SyncError != "boom" {
		t.Fatalf("workload = %+v", w)
	}
	unknown, _ := identity.NewWorkloadID()
	if err := s.SetWorkloadMode(ctx, unknown, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED); !errors.Is(err, registry.ErrWorkloadUnknown) {
		t.Fatalf("unknown err = %v", err)
	}

	// Render transaction: inputs reflect everything above; policies persist
	// with versions and announce on the channel.
	notified := make(chan compiler.Announcement, 8)
	ready := make(chan struct{})
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = s.ListenPolicyChanges(lctx, nil, func() { close(ready) }, func(c compiler.Announcement) { notified <- c })
	}()
	<-ready

	eng := &compiler.Engine{Store: s}
	rep, err := eng.Render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Workloads != 1 || len(rep.Changed) != 1 || rep.Changed[0].Version != 1 {
		t.Fatalf("report = %+v", rep) // mode is ENFORCED, which differs from the empty policy
	}
	select {
	case c := <-notified:
		if c.ID != id || c.Version != 1 {
			t.Fatalf("notification = %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
	}
	p, err := s.GetWorkloadPolicy(ctx, id)
	if err != nil || p == nil || p.GetVersion() != 1 || p.GetMode() != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
		t.Fatalf("policy = %v err=%v", p, err)
	}
	// Re-render: no change, no bump, no notification.
	rep, err = eng.Render(ctx)
	if err != nil || len(rep.Changed) != 0 {
		t.Fatalf("second render = %+v err=%v", rep, err)
	}
	select {
	case c := <-notified:
		t.Fatalf("unexpected notification %+v", c)
	case <-time.After(300 * time.Millisecond):
	}
	if p, _ = s.GetWorkloadPolicy(ctx, id); p.GetVersion() != 1 {
		t.Fatalf("version moved to %d without a change", p.GetVersion())
	}
	if unknownPolicy, err := s.GetWorkloadPolicy(ctx, unknown); err != nil || unknownPolicy != nil {
		t.Fatalf("unknown policy = %v err=%v", unknownPolicy, err)
	}
	if !rendered.Equal(p, &innerwallv1.WorkloadPolicy{Mode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED}) {
		t.Fatalf("policy content = %v", p)
	}
}
