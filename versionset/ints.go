// SPDX-License-Identifier: Apache-2.0 OR MIT

package versionset

import (
	"sort"
	"strconv"
	"strings"
)

// Ints is a reference Set over int64 "versions", stored as sorted, disjoint,
// non-adjacent half-open intervals.
//
// It exists for two reasons: it makes the Set contract concrete enough to read,
// and it gives the solver's own tests a set implementation that is obviously
// correct. A real ecosystem supplies its own — a PEP 440 or SemVer range set —
// rather than mapping versions onto integers.
//
// The zero value is the empty set and is ready to use.
//
// # Canonical form
//
// Every constructor and operation normalizes: intervals are sorted, and any
// that overlap OR merely touch are merged, so [1,2) ∪ [2,3) becomes [1,3).
// Merging touching intervals is what makes Equal a reliable test of logical
// equality, which the Set documentation explains the solver depends on. Two
// Ints denote the same versions exactly when their interval slices are
// identical.
type Ints struct {
	// spans holds [lo, hi) pairs in ascending order, disjoint and
	// non-adjacent. A nil or empty slice is the empty set.
	spans []span
}

type span struct{ lo, hi int64 }

// minInt64 and maxInt64 bound the universe, so Complement has something to
// complement against. A version outside this range cannot be represented, which
// is acceptable for a reference implementation.
const (
	minInt64 = -1 << 63
	maxInt64 = 1<<63 - 1
)

// Empty returns the empty set.
func Empty() Ints { return Ints{} }

// All returns the set of every representable version.
func All() Ints { return Ints{spans: []span{{minInt64, maxInt64}}} }

// Exactly returns the set containing only v.
//
// A decision pins a package to one version, which the solver represents as a
// singleton set rather than as a separate version type. That is why the solver
// needs no notion of an individual version at all.
func Exactly(v int64) Ints {
	if v == maxInt64 {
		// The universe's upper bound is exclusive everywhere else; a singleton
		// at the maximum cannot be expressed as [v, v+1) without overflow.
		return Ints{spans: []span{{v, maxInt64}}}
	}
	return Ints{spans: []span{{v, v + 1}}}
}

// Range returns the half-open set [lo, hi). An empty or inverted range yields
// the empty set.
func Range(lo, hi int64) Ints {
	if lo >= hi {
		return Ints{}
	}
	return Ints{spans: []span{{lo, hi}}}
}

// AtLeast returns [lo, ∞).
func AtLeast(lo int64) Ints { return Ints{spans: []span{{lo, maxInt64}}} }

// LessThan returns (-∞, hi).
func LessThan(hi int64) Ints {
	if hi == minInt64 {
		return Ints{}
	}
	return Ints{spans: []span{{minInt64, hi}}}
}

// Union implements Set.
func (s Ints) Union(other Ints) Ints {
	merged := make([]span, 0, len(s.spans)+len(other.spans))
	merged = append(merged, s.spans...)
	merged = append(merged, other.spans...)
	return Ints{spans: normalize(merged)}
}

// Intersect implements Set.
func (s Ints) Intersect(other Ints) Ints {
	var out []span

	// Both inputs are sorted and disjoint, so a single merge pass suffices.
	i, j := 0, 0
	for i < len(s.spans) && j < len(other.spans) {
		a, b := s.spans[i], other.spans[j]

		lo := max(a.lo, b.lo)
		hi := min(a.hi, b.hi)
		if lo < hi {
			out = append(out, span{lo, hi})
		}

		// Advance whichever interval ends first; the other may still overlap
		// the next one.
		if a.hi < b.hi {
			i++
		} else {
			j++
		}
	}

	return Ints{spans: normalize(out)}
}

// Complement implements Set.
func (s Ints) Complement() Ints {
	if len(s.spans) == 0 {
		return All()
	}

	var out []span
	cursor := int64(minInt64)
	for _, sp := range s.spans {
		if sp.lo > cursor {
			out = append(out, span{cursor, sp.lo})
		}
		if sp.hi > cursor {
			cursor = sp.hi
		}
	}
	if cursor < maxInt64 {
		out = append(out, span{cursor, maxInt64})
	}

	return Ints{spans: normalize(out)}
}

// IsEmpty implements Set.
func (s Ints) IsEmpty() bool { return len(s.spans) == 0 }

// Equal implements Set.
//
// Because both operands are canonical, this is a slice comparison rather than a
// set-theoretic one.
func (s Ints) Equal(other Ints) bool {
	if len(s.spans) != len(other.spans) {
		return false
	}
	for i := range s.spans {
		if s.spans[i] != other.spans[i] {
			return false
		}
	}
	return true
}

// Contains reports whether v is in the set. Not part of Set — the solver does
// not need it — but useful to callers and to tests.
func (s Ints) Contains(v int64) bool {
	for _, sp := range s.spans {
		if v >= sp.lo && v < sp.hi {
			return true
		}
	}
	return false
}

// String renders the set for diagnostics.
func (s Ints) String() string {
	if len(s.spans) == 0 {
		return "∅"
	}

	var b strings.Builder
	for i, sp := range s.spans {
		if i > 0 {
			b.WriteString(" ∪ ")
		}
		switch {
		case sp.lo == minInt64 && sp.hi == maxInt64:
			b.WriteString("*")
		case sp.lo == minInt64:
			b.WriteString("<" + strconv.FormatInt(sp.hi, 10))
		case sp.hi == maxInt64:
			b.WriteString(">=" + strconv.FormatInt(sp.lo, 10))
		case sp.hi == sp.lo+1:
			b.WriteString(strconv.FormatInt(sp.lo, 10))
		default:
			b.WriteString("[" + strconv.FormatInt(sp.lo, 10) + "," + strconv.FormatInt(sp.hi, 10) + ")")
		}
	}
	return b.String()
}

// normalize sorts spans and merges every pair that overlaps or touches,
// producing the canonical form Equal depends on.
func normalize(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].lo != spans[j].lo {
			return spans[i].lo < spans[j].lo
		}
		return spans[i].hi < spans[j].hi
	})

	out := make([]span, 0, len(spans))
	cur := spans[0]
	for _, sp := range spans[1:] {
		if sp.lo > cur.hi {
			// Genuinely separated: keep them apart.
			out = append(out, cur)
			cur = sp
			continue
		}
		// Overlapping OR touching (sp.lo == cur.hi) — merge. Merging the
		// touching case is what makes [1,2) ∪ [2,3) equal [1,3), which Equal
		// relies on.
		if sp.hi > cur.hi {
			cur.hi = sp.hi
		}
	}
	out = append(out, cur)

	// Drop degenerate intervals that carry no versions.
	final := out[:0]
	for _, sp := range out {
		if sp.lo < sp.hi {
			final = append(final, sp)
		}
	}
	if len(final) == 0 {
		return nil
	}
	return final
}

// Compile-time assertion that Ints satisfies the abstraction it documents.
var _ Set[Ints] = Ints{}
