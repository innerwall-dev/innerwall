package fleet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// Errors a caller branches on.
var (
	// ErrMatchCountMismatch is returned when a mode change's expected
	// match count is not what the selector resolved to; the error
	// carrying it is a *MatchCountError with both numbers.
	ErrMatchCountMismatch = errors.New("fleet: the selector did not resolve to the expected number of workloads")
	// ErrBadRequest is the kind of every admission failure of a request
	// here; the error carrying it is a *policy.Findings with the paths.
	ErrBadRequest = errors.New("fleet: the request is not admissible")
)

// MatchCountError is a refused mode change: the operator expected one
// number of workloads and the control plane resolved another. Both are
// reported so the operator can see what moved.
type MatchCountError struct {
	Expected int
	Matched  int
}

func (e *MatchCountError) Error() string {
	return fmt.Sprintf("%s (expected %d, matched %d)", ErrMatchCountMismatch.Error(), e.Expected, e.Matched)
}

func (e *MatchCountError) Unwrap() error { return ErrMatchCountMismatch }

// ModeChangeTx is the store's side of one mode change: the render
// transaction, plus the recording of the intent and the flip of the
// resolved set by explicit id.
type ModeChangeTx interface {
	compiler.Tx
	// RecordModeChange persists the intent and the set it resolved to.
	RecordModeChange(ctx context.Context, rec *ModeChangeRecord) error
	// SetWorkloadModes sets the desired mode of exactly the given
	// workloads and reports how many were not already in it.
	SetWorkloadModes(ctx context.Context, ids []identity.WorkloadID, mode innerwallv1.EnforcementMode) (int, error)
}

// Store is the persistence the fleet domain needs, implemented with
// hand-written SQL in internal/store (ADR-0006).
type Store interface {
	// ListWorkloads returns every workload with its labels.
	ListWorkloads(ctx context.Context) ([]registry.Workload, error)
	// ReplaceWorkloadLabels replaces a workload's labels when they still
	// hold the version the caller read (registry.LabelsVersion), refusing
	// with a *registry.LabelsVersionError otherwise; an empty expect
	// replaces unconditionally. It returns registry.ErrWorkloadUnknown
	// for an unregistered id.
	ReplaceWorkloadLabels(ctx context.Context, id identity.WorkloadID, labels []registry.Label, expect string) error
	// ModeChangeTx runs fn in one transaction holding the render lock and
	// commits when fn returns nil.
	ModeChangeTx(ctx context.Context, fn func(ctx context.Context, tx ModeChangeTx) error) error
	// LoadRenderState reads the render inputs and every persisted policy
	// as one consistent snapshot, without a lock and without writing.
	LoadRenderState(ctx context.Context) (*compiler.Inputs, map[identity.WorkloadID]*innerwallv1.WorkloadPolicy, error)
}

// Service is the fleet write domain.
type Service struct {
	Store  Store
	Engine *compiler.Engine
	// Reads is the read model a directed reconnect judges the workload
	// by, and Directives is where it sends one; RequestReconnect needs
	// both, nothing else here uses them.
	Reads      *readmodel.Reader
	Directives Directives
	// Now is the clock; time.Now if nil.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// WorkloadRef names a workload a selector resolved to.
type WorkloadRef struct {
	ID       identity.WorkloadID
	Hostname string
	Labels   map[string]string
}

// --- labels ------------------------------------------------------------------

// ValidateLabels admits a label set: every key named, no key twice.
func ValidateLabels(labels []registry.Label) error {
	f := &policy.Findings{}
	seen := map[string]int{}
	for i, l := range labels {
		path := fmt.Sprintf("labels[%d]", i)
		if l.Key == "" {
			f.Errors = append(f.Errors, &policy.ValidationError{Path: path, Err: policy.ErrEmptyLabelKey})
			continue
		}
		if first, dup := seen[l.Key]; dup {
			f.Errors = append(f.Errors, &policy.ValidationError{Path: path, Err: ErrDuplicateLabelKey, Detail: fmt.Sprintf("%q also at labels[%d]", l.Key, first)})
			continue
		}
		seen[l.Key] = i
	}
	if len(f.Errors) == 0 {
		return nil
	}
	return f
}

// ErrDuplicateLabelKey is a label key given twice in one set.
var ErrDuplicateLabelKey = errors.New("fleet: label key appears more than once")

// SetLabels replaces a workload's labels and renders, since a label change
// moves the workload in and out of selectors (ADR-0018). expect is the
// version of the labels the caller read; empty writes unconditionally.
func (s *Service) SetLabels(ctx context.Context, id identity.WorkloadID, labels []registry.Label, expect string) error {
	if err := ValidateLabels(labels); err != nil {
		return err
	}
	if err := s.Store.ReplaceWorkloadLabels(ctx, id, labels, expect); err != nil {
		return err
	}
	if _, err := s.Engine.Render(ctx); err != nil {
		return fmt.Errorf("fleet: labels persisted but rendering failed: %w", err)
	}
	return nil
}

// --- selector preview --------------------------------------------------------

// Preview is what a selector resolves to now.
type Preview struct {
	Matched []WorkloadRef
}

// PreviewSelector resolves a selector against the registry through the
// renderer's scope match, exactly as a mode change resolves it.
func (s *Service) PreviewSelector(ctx context.Context, sel policy.Selector) (*Preview, error) {
	if err := policy.ValidateSelector(sel); err != nil {
		return nil, rebase(err, "selector")
	}
	workloads, err := s.Store.ListWorkloads(ctx)
	if err != nil {
		return nil, err
	}
	return &Preview{Matched: resolve(sel, workloads)}, nil
}

// resolve applies the scope match to a set of workloads.
func resolve(sel policy.Selector, workloads []registry.Workload) []WorkloadRef {
	index := make(map[identity.WorkloadID]map[string]string, len(workloads))
	byID := make(map[identity.WorkloadID]*registry.Workload, len(workloads))
	for i := range workloads {
		index[workloads[i].ID] = workloads[i].LabelMap()
		byID[workloads[i].ID] = &workloads[i]
	}
	out := make([]WorkloadRef, 0)
	for _, id := range policy.MatchWorkloads(sel, index) {
		w := byID[id]
		out = append(out, WorkloadRef{ID: id, Hostname: w.Hostname, Labels: w.LabelMap()})
	}
	return out
}

// rebase prefixes the paths of the findings err carries.
func rebase(err error, prefix string) error {
	f := policy.AsFindings(err)
	if f == nil {
		return err
	}
	for _, e := range f.Errors {
		switch {
		case e.Path == "":
			e.Path = prefix
		case e.Path[0] == '[':
			e.Path = prefix + e.Path
		default:
			e.Path = prefix + "." + e.Path
		}
	}
	return f
}

// --- mode change -------------------------------------------------------------

// ModeChangeRequest is what the operator submits: exactly one of a
// selector or a list of workload ids, the mode to reach, and the number
// of workloads the operator expects the selection to be.
type ModeChangeRequest struct {
	Selector           policy.Selector
	WorkloadIDs        []identity.WorkloadID
	TargetMode         innerwallv1.EnforcementMode
	ExpectedMatchCount int
}

// ModeChangeWorkload is one workload of a recorded set with the mode it
// held before the change.
type ModeChangeWorkload struct {
	ID           identity.WorkloadID
	PreviousMode innerwallv1.EnforcementMode
}

// ModeChangeRecord is the recorded intent: the request as submitted and
// the set it resolved to at the instant it was applied.
type ModeChangeRecord struct {
	ID                 uuid.UUID
	CreatedAt          time.Time
	TargetMode         innerwallv1.EnforcementMode
	Selector           policy.Selector
	ExpectedMatchCount int
	Workloads          []ModeChangeWorkload
	DesiredUpdated     int
}

// ModeChangeResult acknowledges a recorded mode change. Matched is the
// size of the resolved set; DesiredUpdated is how many of them were not
// already in the target mode. Rendered is the render's report: which
// workloads' versions advanced. None of it says anything about
// convergence.
type ModeChangeResult struct {
	ID             uuid.UUID
	Matched        int
	DesiredUpdated int
	Rendered       *compiler.Report
}

// Errors of a mode change request's shape.
var (
	ErrModeChangeSelection = errors.New("fleet: exactly one of selector or workload_ids must be given")
	ErrModeChangeMode      = errors.New("fleet: target_mode must be visibility, simulation, or enforced")
	ErrModeChangeExpected  = errors.New("fleet: expected_match_count is required and must not be negative")
	ErrUnknownWorkload     = errors.New("fleet: workload is not registered")
)

// validateModeChange admits the request's shape. HasExpected says whether
// the caller stated an expected count at all, which a transport must
// know separately from its value.
func validateModeChange(req *ModeChangeRequest, hasExpected bool) error {
	f := &policy.Findings{}
	add := func(path string, err error, detail string) {
		f.Errors = append(f.Errors, &policy.ValidationError{Path: path, Err: err, Detail: detail})
	}
	switch {
	case len(req.Selector) > 0 && len(req.WorkloadIDs) > 0, len(req.Selector) == 0 && len(req.WorkloadIDs) == 0:
		add("selector", ErrModeChangeSelection, "")
	case len(req.Selector) > 0:
		if err := policy.ValidateSelector(req.Selector); err != nil {
			f.Errors = append(f.Errors, policy.AsFindings(rebase(err, "selector")).Errors...)
		}
	}
	switch req.TargetMode {
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_VISIBILITY, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_SIMULATION, innerwallv1.EnforcementMode_ENFORCEMENT_MODE_ENFORCED:
	case innerwallv1.EnforcementMode_ENFORCEMENT_MODE_UNSPECIFIED:
		add("target_mode", ErrModeChangeMode, "")
	default:
		add("target_mode", ErrModeChangeMode, fmt.Sprintf("unknown mode %d", req.TargetMode))
	}
	if !hasExpected || req.ExpectedMatchCount < 0 {
		add("expected_match_count", ErrModeChangeExpected, "")
	}
	if len(f.Errors) == 0 {
		return nil
	}
	return f
}

// ChangeMode applies a mode change: in one transaction under the render
// lock it resolves the selection against the registry as it stands,
// refuses when the resolved count is not the expected one, records the
// intent with the resolved set, flips the desired mode of exactly that
// set by id, and renders so that every affected workload's version
// advances and is announced on commit. A label change committed by
// another process meanwhile cannot move the set: the flip names the ids
// the resolution produced, never the selector again.
func (s *Service) ChangeMode(ctx context.Context, req ModeChangeRequest, hasExpected bool) (*ModeChangeResult, error) {
	if err := validateModeChange(&req, hasExpected); err != nil {
		return nil, err
	}
	res := &ModeChangeResult{}
	err := s.Store.ModeChangeTx(ctx, func(ctx context.Context, tx ModeChangeTx) error {
		in, err := tx.LoadInputs(ctx)
		if err != nil {
			return err
		}
		byID := make(map[identity.WorkloadID]*registry.Workload, len(in.Workloads))
		for i := range in.Workloads {
			byID[in.Workloads[i].ID] = &in.Workloads[i]
		}
		var resolved []identity.WorkloadID
		if len(req.Selector) > 0 {
			for _, ref := range resolve(req.Selector, in.Workloads) {
				resolved = append(resolved, ref.ID)
			}
		} else {
			f := &policy.Findings{}
			seen := map[identity.WorkloadID]bool{}
			for i, id := range req.WorkloadIDs {
				if _, ok := byID[id]; !ok {
					f.Errors = append(f.Errors, &policy.ValidationError{Path: fmt.Sprintf("workload_ids[%d]", i), Err: ErrUnknownWorkload, Detail: id.String()})
					continue
				}
				if !seen[id] {
					seen[id] = true
					resolved = append(resolved, id)
				}
			}
			if len(f.Errors) > 0 {
				return f
			}
		}
		if len(resolved) != req.ExpectedMatchCount {
			return &MatchCountError{Expected: req.ExpectedMatchCount, Matched: len(resolved)}
		}
		rec := &ModeChangeRecord{ID: uuid.New(), CreatedAt: s.now(), TargetMode: req.TargetMode, Selector: req.Selector, ExpectedMatchCount: req.ExpectedMatchCount}
		for _, id := range resolved {
			rec.Workloads = append(rec.Workloads, ModeChangeWorkload{ID: id, PreviousMode: byID[id].Mode})
			if byID[id].Mode != req.TargetMode {
				rec.DesiredUpdated++
			}
		}
		if err := tx.RecordModeChange(ctx, rec); err != nil {
			return err
		}
		updated, err := tx.SetWorkloadModes(ctx, resolved, req.TargetMode)
		if err != nil {
			return err
		}
		if updated != rec.DesiredUpdated {
			// The set was resolved and flipped in this transaction; a
			// different count means the rows moved under the lock, which
			// nothing may do.
			return fmt.Errorf("fleet: flipped %d workloads, resolved %d needing the change", updated, rec.DesiredUpdated)
		}
		report, err := s.Engine.RenderIn(ctx, tx)
		if err != nil {
			return err
		}
		res.ID, res.Matched, res.DesiredUpdated, res.Rendered = rec.ID, len(resolved), rec.DesiredUpdated, report
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// --- dry run -----------------------------------------------------------------

// DryRunRequest is a hypothetical policy set: the complete set of
// rulesets as they would be authored (services and address groups are
// the persisted ones, referenced by id), and the state version the
// author read before editing, if any.
type DryRunRequest struct {
	Rulesets     []policy.Ruleset
	StateVersion string
}

// DryRunResult is what rendering the hypothetical set would change,
// against the state version it was computed on. Stale reports whether
// the request named a different version, so the author knows the state
// moved under them; the diff is still against what is persisted now.
type DryRunResult struct {
	StateVersion string
	Stale        bool
	Workloads    []compiler.WorkloadDiff
}

// DryRun renders the hypothetical set against persisted state and diffs
// it against the persisted rendered policies, then discards everything.
// The hypothetical rulesets are admitted exactly as a write would admit
// them, so a dry run that passes here is one the write path would accept.
func (s *Service) DryRun(ctx context.Context, req DryRunRequest) (*DryRunResult, error) {
	in, previous, err := s.Store.LoadRenderState(ctx)
	if err != nil {
		return nil, err
	}
	refs := policy.References{Services: map[uuid.UUID]struct{}{}, AddressGroups: map[uuid.UUID]struct{}{}}
	for i := range in.Services {
		refs.Services[in.Services[i].ID] = struct{}{}
	}
	for i := range in.AddressGroups {
		refs.AddressGroups[in.AddressGroups[i].ID] = struct{}{}
	}
	f := &policy.Findings{}
	names := map[string]int{}
	for i := range req.Rulesets {
		rs := &req.Rulesets[i]
		if err := policy.ValidateRuleset(rs, refs); err != nil {
			f.Errors = append(f.Errors, policy.AsFindings(rebase(err, fmt.Sprintf("rulesets[%d]", i))).Errors...)
		}
		if first, dup := names[rs.Name]; dup && rs.Name != "" {
			f.Errors = append(f.Errors, &policy.ValidationError{Path: fmt.Sprintf("rulesets[%d].name", i), Err: policy.ErrDuplicateName, Detail: fmt.Sprintf("also rulesets[%d]", first)})
		} else {
			names[rs.Name] = i
		}
	}
	if len(f.Errors) > 0 {
		return nil, f
	}
	res := &DryRunResult{StateVersion: compiler.StateVersion(in)}
	res.Stale = req.StateVersion != "" && req.StateVersion != res.StateVersion
	hypothetical := *in
	hypothetical.Rulesets = req.Rulesets
	res.Workloads = compiler.DryRun(previous, compiler.Render(&hypothetical))
	return res, nil
}

// StateVersion is the version of the state a render reads now, for a
// caller that will author against it.
func (s *Service) StateVersion(ctx context.Context) (string, error) {
	in, _, err := s.Store.LoadRenderState(ctx)
	if err != nil {
		return "", err
	}
	return compiler.StateVersion(in), nil
}
