package nft

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/innerwall-dev/innerwall/internal/agent/enforce"
	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/rendered"
)

// MarksFileName is the file in the agent state directory, beside the
// policy file, that holds the mark allocation.
const MarksFileName = "marks.bin"

// MarksPath returns the mark allocation's path inside a state directory.
func MarksPath(stateDir string) string { return filepath.Join(stateDir, MarksFileName) }

// ErrMarksExhausted is returned when a policy's new rules cannot all be
// given a value: every value is held by a rule of the incoming policy or
// of the installed one. Only two consecutive policies that each come near
// MaxRules with different rules can reach it.
var ErrMarksExhausted = errors.New("nft: every connection-mark value is held by a rule of the incoming or the installed policy")

// ErrCorruptMarksFile is returned when the mark allocation file exists but
// does not verify.
var ErrCorruptMarksFile = errors.New("nft: persisted mark allocation is corrupt")

var marksMagic = []byte("innerwall-marks-v1\n")

// Allocator gives every resolved rule a stable value in the mark region
// (ADR-0020). A rule keeps its value for as long as the allocation
// remembers it: a policy change never renumbers a rule, so a connection
// the kernel marked under an earlier version still names the rule that
// admitted it. A rule that leaves the policy frees its value but keeps
// it: values are handed out in increasing order and a freed one is not
// given to another rule until every value has been used once, and then
// the value freed longest ago goes first. Until that happens a freed
// value still names the removed rule, so a connection that outlived its
// rule is attributed to that rule and never to another. The boundary is
// documented: past MaxRules values handed out over the agent's life, a
// connection older than the reuse of its value attributes to the value's
// new holder.
//
// The allocation is persisted beside the policy file and saved before it,
// so a rule's value survives restarts with the policy that uses it.
type Allocator struct {
	limit  uint32
	next   uint32
	gen    uint64
	byID   map[string]*slot
	byMark map[uint32]string
}

type slot struct {
	mark uint32
	// gen is the allocation that last carried the rule; a slot whose gen
	// is older than the allocator's is free.
	gen uint64
}

// NewAllocator returns an empty allocation over the whole region.
func NewAllocator() *Allocator { return newAllocator(uint32(MaxRules)) }

func newAllocator(limit uint32) *Allocator {
	return &Allocator{limit: limit, next: 1, byID: map[string]*slot{}, byMark: map[uint32]string{}}
}

// Clone returns an independent copy.
func (a *Allocator) Clone() *Allocator {
	out := &Allocator{limit: a.limit, next: a.next, gen: a.gen, byID: make(map[string]*slot, len(a.byID)), byMark: make(map[uint32]string, len(a.byMark))}
	for id, s := range a.byID {
		c := *s
		out.byID[id] = &c
	}
	for m, id := range a.byMark {
		out.byMark[m] = id
	}
	return out
}

// Allocate gives every rule of next a value and returns them by rule id,
// for rendering. A rule the allocation already knows keeps its value;
// a new rule takes the lowest value never handed out, or once those are
// gone, the value freed longest ago. A value held by a rule of installed,
// the policy the kernel holds now, is never taken, because live
// connections may carry it. On error the allocator may be partly updated;
// the store allocates on a clone.
func (a *Allocator) Allocate(next, installed *innerwallv1.WorkloadPolicy) (map[string]uint32, error) {
	rules := rendered.Canonical(next).GetInboundRules()
	a.gen++
	out := make(map[string]uint32, len(rules))
	var fresh []string
	for _, r := range rules {
		id := r.GetRuleId()
		if s, ok := a.byID[id]; ok {
			s.gen = a.gen
			out[id] = s.mark
			continue
		}
		fresh = append(fresh, id)
	}
	var reusable []uint32
	reuseFrom := -1
	for _, id := range fresh {
		var mark uint32
		if a.next <= a.limit {
			mark = a.next
			a.next++
		} else {
			if reuseFrom < 0 {
				reusable = a.reusable(installed)
				reuseFrom = 0
			}
			if reuseFrom >= len(reusable) {
				return nil, fmt.Errorf("%w: %d new rules", ErrMarksExhausted, len(fresh))
			}
			mark = reusable[reuseFrom]
			reuseFrom++
			delete(a.byID, a.byMark[mark])
		}
		a.byID[id] = &slot{mark: mark, gen: a.gen}
		a.byMark[mark] = id
		out[id] = mark
	}
	return out, nil
}

// reusable returns the values free to hand to another rule, freed longest
// ago first: held by no rule of the current allocation and by no rule of
// installed.
func (a *Allocator) reusable(installed *innerwallv1.WorkloadPolicy) []uint32 {
	live := map[string]bool{}
	for _, r := range installed.GetInboundRules() {
		live[r.GetRuleId()] = true
	}
	type cand struct {
		mark uint32
		gen  uint64
	}
	var cands []cand
	for id, s := range a.byID {
		if s.gen < a.gen && !live[id] {
			cands = append(cands, cand{s.mark, s.gen})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].gen != cands[j].gen {
			return cands[i].gen < cands[j].gen
		}
		return cands[i].mark < cands[j].mark
	})
	out := make([]uint32, len(cands))
	for i, c := range cands {
		out[i] = c.mark
	}
	return out
}

// Rule returns the rule a region value names: the rule that holds it, or
// the removed rule that held it last and still keeps it.
func (a *Allocator) Rule(index uint32) (string, bool) {
	id, ok := a.byMark[index]
	return id, ok
}

// Mark returns a rule's value, if the allocation knows the rule.
func (a *Allocator) Mark(ruleID string) (uint32, bool) {
	s, ok := a.byID[ruleID]
	if !ok {
		return 0, false
	}
	return s.mark, true
}

type marksFile struct {
	Next       uint32      `json:"next"`
	Generation uint64      `json:"generation"`
	Rules      []marksRule `json:"rules"`
}

type marksRule struct {
	ID         string `json:"id"`
	Mark       uint32 `json:"mark"`
	Generation uint64 `json:"generation"`
}

// Save writes the allocation durably to path, under a magic line and a
// SHA-256 trailer like the policy file.
func (a *Allocator) Save(path string) error {
	f := marksFile{Next: a.next, Generation: a.gen, Rules: make([]marksRule, 0, len(a.byID))}
	for id, s := range a.byID {
		f.Rules = append(f.Rules, marksRule{ID: id, Mark: s.mark, Generation: s.gen})
	}
	sort.Slice(f.Rules, func(i, j int) bool { return f.Rules[i].Mark < f.Rules[j].Mark })
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	data := make([]byte, 0, len(marksMagic)+len(body)+len(sum))
	data = append(data, marksMagic...)
	data = append(data, body...)
	data = append(data, sum[:]...)
	return enforce.WriteDurable(path, data)
}

// LoadAllocator reads the allocation at path. It returns nil, nil when no
// file exists and ErrCorruptMarksFile when one exists but does not verify
// or does not describe a consistent allocation.
func LoadAllocator(path string) (*Allocator, error) {
	return loadAllocator(path, uint32(MaxRules))
}

func loadAllocator(path string, limit uint32) (*Allocator, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the agent's own state file
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("nft: reading mark allocation: %w", err)
	}
	if len(data) < len(marksMagic)+sha256.Size || !bytes.Equal(data[:len(marksMagic)], marksMagic) {
		return nil, fmt.Errorf("%w: %s", ErrCorruptMarksFile, path)
	}
	body := data[len(marksMagic) : len(data)-sha256.Size]
	if sum := sha256.Sum256(body); !bytes.Equal(sum[:], data[len(data)-sha256.Size:]) {
		return nil, fmt.Errorf("%w: %s: checksum mismatch", ErrCorruptMarksFile, path)
	}
	var f marksFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrCorruptMarksFile, path, err)
	}
	a := newAllocator(limit)
	a.next, a.gen = f.Next, f.Generation
	if a.next < 1 || a.next > limit+1 {
		return nil, fmt.Errorf("%w: %s: next value %d", ErrCorruptMarksFile, path, f.Next)
	}
	for _, r := range f.Rules {
		_, dupID := a.byID[r.ID]
		_, dupMark := a.byMark[r.Mark]
		if r.Mark < 1 || r.Mark >= a.next || r.Generation > a.gen || dupID || dupMark {
			return nil, fmt.Errorf("%w: %s: rule %q", ErrCorruptMarksFile, path, r.ID)
		}
		a.byID[r.ID] = &slot{mark: r.Mark, gen: r.Generation}
		a.byMark[r.Mark] = r.ID
	}
	return a, nil
}
