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
	ErrInUse               = errors.New("policy: object is referenced by a rule and cannot be deleted")
	ErrDuplicateName       = errors.New("policy: an object of this kind already has that name")
)

// Store is the persistence the authored model needs. Every method is
// implemented with hand-written SQL in internal/store (ADR-0006). Writes of
// a composite object (a service with its entries, a ruleset with its rules)
// are atomic: the object is either fully persisted or not at all.
type Store interface {
	CreateService(ctx context.Context, s *Service) error
	UpdateService(ctx context.Context, s *Service) error
	// DeleteService returns ErrInUse when a rule references the service.
	DeleteService(ctx context.Context, id uuid.UUID) error
	GetService(ctx context.Context, id uuid.UUID) (*Service, error)
	ListServices(ctx context.Context) ([]Service, error)

	CreateAddressGroup(ctx context.Context, g *AddressGroup) error
	UpdateAddressGroup(ctx context.Context, g *AddressGroup) error
	// DeleteAddressGroup returns ErrInUse when a rule references the group.
	DeleteAddressGroup(ctx context.Context, id uuid.UUID) error
	GetAddressGroup(ctx context.Context, id uuid.UUID) (*AddressGroup, error)
	ListAddressGroups(ctx context.Context) ([]AddressGroup, error)

	CreateRuleset(ctx context.Context, rs *Ruleset) error
	// UpdateRuleset replaces the ruleset and all of its rules.
	UpdateRuleset(ctx context.Context, rs *Ruleset) error
	DeleteRuleset(ctx context.Context, id uuid.UUID) error
	GetRuleset(ctx context.Context, id uuid.UUID) (*Ruleset, error)
	ListRulesets(ctx context.Context) ([]Ruleset, error)
}
