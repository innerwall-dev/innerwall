package policy

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Store errors.
var (
	ErrServiceUnknown      = errors.New("policy: service does not exist")
	ErrAddressGroupUnknown = errors.New("policy: address group does not exist")
	ErrRulesetUnknown      = errors.New("policy: ruleset does not exist")
	ErrRuleUnknown         = errors.New("policy: rule does not exist in this ruleset")
	ErrInUse               = errors.New("policy: object is referenced by a rule and cannot be deleted")
	ErrDuplicateName       = errors.New("policy: an object of this kind already has that name")
	ErrDuplicateRuleID     = errors.New("policy: a rule with that id already exists")
)

// Store is the persistence the authored model needs. Every method is
// implemented with hand-written SQL in internal/store (ADR-0006). Writes of
// a composite object (a service with its entries, a ruleset with its rules)
// are atomic: the object is either fully persisted or not at all.
//
// Updates and deletes are conditional: expect is the version the caller
// last read (VersionOf its UpdatedAt), and the write applies only when the
// object still holds it, refusing otherwise with a *VersionMismatchError
// that names the current version. An empty expect writes unconditionally.
// The check is made inside the write, so two callers holding the same
// version cannot both succeed.
type Store interface {
	CreateService(ctx context.Context, s *Service) error
	UpdateService(ctx context.Context, s *Service, expect string) error
	// DeleteService returns ErrInUse when a rule references the service.
	DeleteService(ctx context.Context, id uuid.UUID, expect string) error
	GetService(ctx context.Context, id uuid.UUID) (*Service, error)
	ListServices(ctx context.Context) ([]Service, error)

	CreateAddressGroup(ctx context.Context, g *AddressGroup) error
	UpdateAddressGroup(ctx context.Context, g *AddressGroup, expect string) error
	// DeleteAddressGroup returns ErrInUse when a rule references the group.
	DeleteAddressGroup(ctx context.Context, id uuid.UUID, expect string) error
	GetAddressGroup(ctx context.Context, id uuid.UUID) (*AddressGroup, error)
	ListAddressGroups(ctx context.Context) ([]AddressGroup, error)

	CreateRuleset(ctx context.Context, rs *Ruleset) error
	// UpdateRuleset replaces the ruleset and all of its rules, persisting
	// each rule's timestamps as given.
	UpdateRuleset(ctx context.Context, rs *Ruleset, expect string) error
	DeleteRuleset(ctx context.Context, id uuid.UUID, expect string) error
	GetRuleset(ctx context.Context, id uuid.UUID) (*Ruleset, error)
	ListRulesets(ctx context.Context) ([]Ruleset, error)
}
