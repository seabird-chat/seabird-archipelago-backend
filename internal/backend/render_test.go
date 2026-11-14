package backend

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
)

type fakeNames struct{}

func (fakeNames) PlayerName(slot int) string { return map[int]string{1: "Alice", 2: "Bob"}[slot] }

func (fakeNames) ItemName(slot int, id int64) string { return fmt.Sprintf("item-%d-%d", slot, id) }

func (fakeNames) LocationName(slot int, id int64) string { return fmt.Sprintf("loc-%d-%d", slot, id) }

func TestRenderParts(t *testing.T) {
	parts := []archipelago.JSONMessagePart{
		{Type: archipelago.PartPlayerID, Text: "1"},
		{Text: " sent "},
		{Type: archipelago.PartItemID, Text: "10", Player: 2, Flags: 1},
		{Text: " to "},
		{Type: archipelago.PartPlayerID, Text: "2"},
		{Text: " ("},
		{Type: archipelago.PartLocationID, Text: "40", Player: 1},
		{Text: ")"},
	}
	block := renderParts(fakeNames{}, parts)
	assert.Equal(t, "Alice sent item-2-10 to Bob (loc-1-40)", block.Plain)

	inner := block.GetContainer().GetInner()
	assert.NotNil(t, inner[0].GetBold(), "players are bold")
	assert.NotNil(t, inner[2].GetItalics(), "items are italic")
}

func TestRenderLifecycle(t *testing.T) {
	msg := archipelago.PrintJSON{
		Type: archipelago.PrintJoin,
		Slot: 1,
		Data: []archipelago.JSONMessagePart{{Text: "Alice (Team #1) playing Game A has joined. Client(0.6.1), ['AP']."}},
	}
	block := renderLifecycle(fakeNames{}, msg)
	assert.Equal(t, "Alice (Team #1) playing Game A has joined.", block.Plain)
	assert.NotNil(t, block.GetContainer().GetInner()[0].GetBold())

	msg = archipelago.PrintJSON{Type: archipelago.PrintGoal, Slot: 9, Data: []archipelago.JSONMessagePart{{Text: "Zed has completed their goal."}}}
	assert.Equal(t, "Zed has completed their goal.", renderLifecycle(fakeNames{}, msg).Plain)
}

func TestRenderHint(t *testing.T) {
	h := archipelago.Hint{ReceivingPlayer: 2, FindingPlayer: 1, Item: 10, Location: 40}
	assert.Equal(t, "[Hint] Bob's item-2-10 is at loc-1-40 in Alice's World.", renderHint(fakeNames{}, h).Plain)

	h.Entrance = "Dark Cave"
	assert.Equal(t, "[Hint] Bob's item-2-10 is at loc-1-40 in Alice's World at Dark Cave.", renderHint(fakeNames{}, h).Plain)
}

func TestRenderDeathLink(t *testing.T) {
	tests := []struct {
		name  string
		death archipelago.DeathLink
		want  string
	}{
		{"no cause", archipelago.DeathLink{Source: "Alice"}, "[DeathLink] Alice died."},
		{"cause with name", archipelago.DeathLink{Source: "Alice", Cause: "Alice was run over by a train."}, "[DeathLink] Alice was run over by a train."},
		{"cause without name", archipelago.DeathLink{Source: "Alice", Cause: "Squashed."}, "[DeathLink] Squashed."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderDeathLink(tt.death).Plain)
		})
	}
}
