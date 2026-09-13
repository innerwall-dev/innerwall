package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/fleet"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/registry"
	"github.com/innerwall-dev/innerwall/internal/storetest"
)

// TestModeChangeTransaction runs a bulk mode change against the seeded
// fleet: the record and its resolved set are persisted, exactly that set
// moves, the render inside the transaction advances the versions of the
// workloads whose mode changed, and a refused change leaves nothing.
func TestModeChangeTransaction(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)
	engine := &compiler.Engine{Store: s}
	svc := &fleet.Service{Store: s, Engine: engine, Now: func() time.Time { return f.Now }}
	versionOf := func(id identity.WorkloadID) uint64 {
		p, err := s.GetWorkloadPolicy(ctx, id)
		if err != nil || p == nil {
			t.Fatalf("policy of %s: %v %v", id, p, err)
		}
		return p.GetVersion()
	}
	webV, dbV, cacheV := versionOf(f.Web), versionOf(f.DB), versionOf(f.Cache)

	_, err := svc.ChangeMode(ctx, fleet.ModeChangeRequest{Selector: policy.Selector{"env": {"prod"}}, TargetMode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, ExpectedMatchCount: 2}, true)
	if !errors.Is(err, fleet.ErrMatchCountMismatch) {
		t.Fatalf("count guard err = %v", err)
	}
	if w, _ := s.LookupWorkload(ctx, f.DB); w.Mode != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION {
		t.Fatal("a refused change moved a mode")
	}

	res, err := svc.ChangeMode(ctx, fleet.ModeChangeRequest{Selector: policy.Selector{"env": {"prod"}}, TargetMode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED, ExpectedMatchCount: 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	// web-1 was already enforced: two updated, two rendered.
	if res.Matched != 3 || res.DesiredUpdated != 2 || len(res.Rendered.Changed) != 2 {
		t.Fatalf("result = %+v", res)
	}
	rec, err := s.GetModeChange(ctx, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.TargetMode != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED || rec.ExpectedMatchCount != 3 || rec.DesiredUpdated != 2 || len(rec.Workloads) != 3 || rec.Selector["env"][0] != "prod" || !rec.CreatedAt.Equal(f.Now) {
		t.Fatalf("record = %+v", rec)
	}
	previous := map[identity.WorkloadID]innerwallv1.EnforcementMode{}
	for _, w := range rec.Workloads {
		previous[w.ID] = w.PreviousMode
	}
	if previous[f.Web] != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED || previous[f.DB] != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION || previous[f.Cache] != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY {
		t.Fatalf("previous modes = %v", previous)
	}
	for _, id := range []identity.WorkloadID{f.Web, f.DB, f.Cache} {
		if w, _ := s.LookupWorkload(ctx, id); w.Mode != innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED {
			t.Fatalf("%s mode = %v", id, w.Mode)
		}
	}
	if versionOf(f.Web) != webV || versionOf(f.DB) != dbV+1 || versionOf(f.Cache) != cacheV+1 {
		t.Fatalf("versions web %d/%d db %d/%d cache %d/%d", versionOf(f.Web), webV, versionOf(f.DB), dbV, versionOf(f.Cache), cacheV)
	}
	// The read model sees the drift the change produced: latest ahead
	// of applied on the workloads that moved.
	rec2, err := s.GetWorkloadRecord(ctx, f.DB)
	if err != nil || rec2.LatestVersion != dbV+1 || rec2.AppliedVersion != 0 {
		t.Fatalf("db record = %+v %v", rec2, err)
	}
	// A change by id records no selector.
	res, err = svc.ChangeMode(ctx, fleet.ModeChangeRequest{WorkloadIDs: []identity.WorkloadID{f.Cache}, TargetMode: innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, ExpectedMatchCount: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	if rec, _ := s.GetModeChange(ctx, res.ID); rec.Selector != nil || len(rec.Workloads) != 1 || rec.Workloads[0].ID != f.Cache {
		t.Fatalf("by-id record = %+v", rec)
	}
}

// TestConditionalWritesAndRuleTimestamps covers the store's side of the
// version checks and the persisted rule instants.
func TestConditionalWritesAndRuleTimestamps(t *testing.T) {
	ctx := context.Background()
	s := storetest.Open(t)
	f := storetest.SeedFleet(t, s)

	rs, err := s.GetRuleset(ctx, f.Ruleset.ID)
	if err != nil {
		t.Fatal(err)
	}
	authoredAt := f.Now.Add(-12 * time.Hour)
	if len(rs.Rules) != 1 || !rs.Rules[0].CreatedAt.Equal(authoredAt) || !rs.Rules[0].UpdatedAt.Equal(authoredAt) {
		t.Fatalf("seeded rule instants = %v %v, want %v", rs.Rules[0].CreatedAt, rs.Rules[0].UpdatedAt, authoredAt)
	}
	current := policy.VersionOf(rs.UpdatedAt)
	rs.Description = "changed"
	rs.UpdatedAt = f.Now
	err = s.UpdateRuleset(ctx, rs, "42")
	var vm *policy.VersionMismatchError
	if !errors.As(err, &vm) || vm.Current != current {
		t.Fatalf("stale update err = %v", err)
	}
	if err := s.UpdateRuleset(ctx, rs, "not-a-version"); !errors.Is(err, policy.ErrVersionMismatch) {
		t.Fatalf("garbage version err = %v", err)
	}
	if err := s.UpdateRuleset(ctx, rs, current); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRuleset(ctx, rs.ID, current); !errors.Is(err, policy.ErrVersionMismatch) {
		t.Fatalf("stale delete err = %v", err)
	}
	unknown := *rs
	unknown.ID = f.Office.ID
	if err := s.UpdateRuleset(ctx, &unknown, current); !errors.Is(err, policy.ErrRulesetUnknown) {
		t.Fatalf("unknown update err = %v", err)
	}
	g := f.Office
	g.Name = "hq"
	g.UpdatedAt = f.Now
	if err := s.UpdateAddressGroup(ctx, &g, "1"); !errors.Is(err, policy.ErrVersionMismatch) {
		t.Fatalf("stale group err = %v", err)
	}
	if err := s.UpdateAddressGroup(ctx, &g, policy.VersionOf(f.Office.UpdatedAt)); err != nil {
		t.Fatal(err)
	}

	// Labels are versioned by content.
	w, _ := s.LookupWorkload(ctx, f.Web)
	err = s.ReplaceWorkloadLabels(ctx, f.Web, []registry.Label{{Key: "role", Value: "api"}}, "stale")
	var lv *registry.LabelsVersionError
	if !errors.As(err, &lv) || lv.Current != registry.LabelsVersion(w.Labels) {
		t.Fatalf("stale labels err = %v", err)
	}
	if err := s.ReplaceWorkloadLabels(ctx, f.Web, []registry.Label{{Key: "role", Value: "api"}}, registry.LabelsVersion(w.Labels)); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.LookupWorkload(ctx, f.Web); len(w.Labels) != 1 || w.Labels[0].Value != "api" {
		t.Fatalf("labels = %v", w.Labels)
	}

	// The read-only snapshot carries the inputs and every policy.
	in, policies, err := s.LoadRenderState(ctx)
	if err != nil || len(in.Workloads) != 3 || len(in.Rulesets) != 1 || len(policies) != 3 {
		t.Fatalf("render state = %d workloads %d rulesets %d policies %v", len(in.Workloads), len(in.Rulesets), len(policies), err)
	}
}
