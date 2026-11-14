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
	ItemUseful
	ItemFiller
	ItemTrap

	ItemAll = ItemProgression | ItemUseful | ItemFiller | ItemTrap
)

var itemClassNames = map[string]ItemFilter{
	"progression": ItemProgression,
	"useful":      ItemUseful,
	"filler":      ItemFiller,
	"trap":        ItemTrap,
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
			return 0, fmt.Errorf("unknown item class %q (want progression, useful, filler, or trap)", name)
		}
		f |= class
	}
	return f, nil
}

// Allows reports whether an item with the given NetworkItem flags should be
// announced. Items with several flags match if any of them is enabled.
func (f ItemFilter) Allows(flags int) bool {
	if flags == 0 {
		return f&ItemFiller != 0
	}
	return (flags&archipelago.FlagProgression != 0 && f&ItemProgression != 0) ||
		(flags&archipelago.FlagUseful != 0 && f&ItemUseful != 0) ||
		(flags&archipelago.FlagTrap != 0 && f&ItemTrap != 0)
}
