package backend

import (
	"fmt"
	"strings"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
)

// ItemFilter selects which item classes are announced.
type ItemFilter uint8

const (
	ItemProgression ItemFilter = 1 << iota
	// Progression that progression balancing leaves alone, usually because
	// it's plentiful (currency, shards) or meant to be spread evenly.
	ItemProgressionSkipBalancing
	ItemUseful
	ItemFiller
	ItemTrap

	ItemAll = ItemProgression | ItemProgressionSkipBalancing | ItemUseful | ItemFiller | ItemTrap
)

var itemClassNames = map[string]ItemFilter{
	"progression":                ItemProgression,
	"progression_skip_balancing": ItemProgressionSkipBalancing,
	"useful":                     ItemUseful,
	"filler":                     ItemFiller,
	"trap":                       ItemTrap,
}

// ParseItemFilter parses a comma separated list of item classes.
func ParseItemFilter(s string) (ItemFilter, error) {
	var f ItemFilter
	for name := range strings.SplitSeq(s, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		class, ok := itemClassNames[name]
		if !ok {
			return 0, fmt.Errorf("unknown item class %q (want progression, progression_skip_balancing, useful, filler, or trap)", name)
		}
		f |= class
	}
	return f, nil
}

// classes maps NetworkItem flags to item classes. Items with none of the
// progression, useful, or trap flags are filler, whatever else is set.
func classes(flags int) ItemFilter {
	var c ItemFilter
	if flags&archipelago.FlagProgression != 0 {
		if flags&archipelago.FlagSkipBalancing != 0 {
			c |= ItemProgressionSkipBalancing
		} else {
			c |= ItemProgression
		}
	}
	if flags&archipelago.FlagUseful != 0 {
		c |= ItemUseful
	}
	if flags&archipelago.FlagTrap != 0 {
		c |= ItemTrap
	}
	if c == 0 {
		c = ItemFiller
	}
	return c
}

// Allows reports whether an item with the given NetworkItem flags should be
// announced. Items in several classes match if any of them is enabled.
func (f ItemFilter) Allows(flags int) bool {
	return f&classes(flags) != 0
}
