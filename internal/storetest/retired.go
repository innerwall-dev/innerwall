package storetest

import (
	"context"
	"net/netip"
	"time"

	"github.com/innerwall-dev/innerwall/internal/authn"
	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/enroll"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/store"
)

// RetiredHost is the hostname of the workload SeedRetired retires.
const RetiredHost = "batch-prod-01"

// SeedRetired adds one retired workload to a fleet Seed has loaded, for
// reviewing the console's retired state against a running control plane:
// batch-prod-01 (app=batch, env=prod, tier=worker), a decommissioned
// batch host whose agent was last heard from thirty hours before f.Now,
// retired four hours after that through the domain function the command
// line and the operator surface call, with its credential lapsed since.
// No rule names its labels, so no other workload's policy moves. It is
// development data only, loaded by the dev seed command beside the
// review estate.
func SeedRetired(ctx context.Context, s *store.Store, f *Fleet) (identity.WorkloadID, error) {
	id, err := identity.NewWorkloadID()
	if err != nil {
		return id, err
	}
	enrolled := f.Now.Add(-40 * 24 * time.Hour)
	labels := []enroll.Label{{Key: "app", Value: "batch"}, {Key: "env", Value: "prod"}, {Key: "tier", Value: "worker"}}
	w := enroll.Workload{ID: id, TokenID: f.Token.ID, Hostname: RetiredHost, Labels: labels, EnrolledAt: enrolled, CredentialSerial: RetiredHost + "-1", CredentialExpiresAt: enrolled.Add(24 * time.Hour)}
	if err := s.CreateWorkload(ctx, w, enrolled); err != nil {
		return id, err
	}
	addr := netip.MustParseAddr("10.80.0.11")
	facts := &innerwallv1.HostFacts{Hostname: RetiredHost, Os: &innerwallv1.OsInfo{Family: "linux", Name: "debian", Version: "13", KernelVersion: "6.12", Architecture: "amd64"}, Interfaces: []*innerwallv1.NetworkInterface{{Name: "eth0", Addresses: []string{addr.String() + "/16"}}}}
	if _, err := s.RecordFacts(ctx, id, facts, enrolled); err != nil {
		return id, err
	}
	// Its last renewal, two days before f.Now, lapsed a day later, while
	// the host was being taken down.
	renewed := f.Now.Add(-50 * time.Hour)
	if err := s.RecordRenewal(ctx, id, RetiredHost+"-2", renewed.Add(24*time.Hour), renewed, nil); err != nil {
		return id, err
	}
	if err := s.SetWorkloadMode(ctx, id, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED); err != nil {
		return id, err
	}
	engine := &compiler.Engine{Store: s}
	if _, err := engine.Render(ctx); err != nil {
		return id, err
	}
	p, err := s.GetWorkloadPolicy(ctx, id)
	if err != nil {
		return id, err
	}
	seen := f.Now.Add(-30 * time.Hour)
	if err := s.RecordAgent(ctx, id, registry.AgentInfo{Version: "0.3.0", Capabilities: []string{"nftables", "conntrack"}}, p.GetVersion(), nil, seen); err != nil {
		return id, err
	}
	if err := s.RecordApplied(ctx, id, p.GetVersion(), innerwallv1.SyncState_SYNC_STATE_SYNCED, seen); err != nil {
		return id, err
	}
	if err := s.RecordHeartbeat(ctx, id, registry.Heartbeat{OwnedTable: innerwallv1.OwnedTableState_OWNED_TABLE_STATE_PRESENT}, seen); err != nil {
		return id, err
	}
	if err := s.SetSyncState(ctx, id, innerwallv1.SyncState_SYNC_STATE_OFFLINE, "", seen); err != nil {
		return id, err
	}
	at := f.Now.Add(-26 * time.Hour)
	svc := &fleet.Service{Store: s, Engine: engine, FlowWindow: f.WindowLength, Now: func() time.Time { return at }}
	if _, err := svc.Retire(authn.NewContext(ctx, &authn.Principal{}), id); err != nil {
		return id, err
	}
	return id, nil
}
