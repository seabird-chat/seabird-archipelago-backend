package backend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseItemFilter(t *testing.T) {
	tests := []struct {
		in      string
		want    ItemFilter
		wantErr bool
	}{
		{in: "progression,progression_skip_balancing,useful,filler,trap", want: ItemAll},
		{in: "progression", want: ItemProgression},
		{in: " Useful , TRAP ", want: ItemUseful | ItemTrap},
		{in: "", want: 0},
		{in: "progression,junk", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseItemFilter(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestItemFilterAllows(t *testing.T) {
	tests := []struct {
		name   string
		filter ItemFilter
		flags  int
		want   bool
	}{
		{"filler allowed", ItemFiller, 0, true},
		{"filler blocked", ItemProgression, 0, false},
		{"progression", ItemProgression, 0b001, true},
		{"useful blocked", ItemProgression, 0b010, false},
		{"progression and useful matches either", ItemUseful, 0b011, true},
		{"trap", ItemTrap, 0b100, true},
		{"trap blocked", ItemAll &^ ItemTrap, 0b100, false},
		{"skip balancing is its own class", ItemProgression, 0b01001, false},
		{"skip balancing allowed", ItemProgressionSkipBalancing, 0b01001, true},
		{"skip balancing useful matches useful", ItemUseful, 0b01011, true},
		{"deprioritized is still progression", ItemProgression, 0b10001, true},
		{"stray skip balancing is filler", ItemFiller, 0b01000, true},
		{"stray deprioritized is filler", ItemFiller, 0b10000, true},
		{"stray flags not progression", ItemProgression | ItemProgressionSkipBalancing, 0b11000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.filter.Allows(tt.flags))
		})
	}
}

func TestSayLines(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{name: "plain", in: "hello there", want: []string{"hello there"}},
		{name: "multi-line", in: "one\n\ntwo\r\n", want: []string{"one", "two"}},
		{name: "unprintable", in: "a\tb\u200dc d", want: []string{"a bc d"}},
		{name: "bang in middle", in: "say !release later", want: []string{"say !release later"}},
		{name: "action", in: "* !waves", want: []string{"* !waves"}},
		{name: "empty", in: " \n ", wantErr: true},
		{name: "command", in: "!release", wantErr: true},
		{name: "leading space", in: "   !release", wantErr: true},
		{name: "non-ascii space", in: "\u00a0!release", wantErr: true},
		{name: "quoted", in: `"!release"`, wantErr: true},
		{name: "single quoted", in: `'!collect'`, wantErr: true},
		{name: "escaped", in: `\!release`, wantErr: true},
		{name: "zero width", in: "\u200b!release", wantErr: true},
		{name: "later line", in: "ok\n!release", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sayLines(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
