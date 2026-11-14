package backend

import (
	"strconv"
	"strings"

	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
)

// names resolves ids to display names for the current room.
type names interface {
	PlayerName(slot int) string
	ItemName(slot int, id int64) string
	LocationName(slot int, id int64) string
}

func player(name string) *pb.Block   { return seabird.NewBoldBlock(seabird.NewTextBlock(name)) }
func item(name string) *pb.Block     { return seabird.NewItalicsBlock(seabird.NewTextBlock(name)) }
func location(name string) *pb.Block { return seabird.NewTextBlock(name) }

// renderParts turns PrintJSON parts into blocks: players bold, items in
// italics, ids resolved through the data package.
func renderParts(n names, parts []archipelago.JSONMessagePart) *pb.Block {
	blocks := make([]*pb.Block, 0, len(parts))
	for _, part := range parts {
		blocks = append(blocks, renderPart(n, part))
	}
	return seabird.NewContainerBlock(blocks...)
}

func renderPart(n names, part archipelago.JSONMessagePart) *pb.Block {
	switch part.Type {
	case archipelago.PartPlayerID:
		if slot, err := strconv.Atoi(part.Text); err == nil {
			return player(n.PlayerName(slot))
		}
		return player(part.Text)
	case archipelago.PartPlayerName:
		return player(part.Text)
	case archipelago.PartItemID:
		if id, err := strconv.ParseInt(part.Text, 10, 64); err == nil {
			return item(n.ItemName(part.Player, id))
		}
		return item(part.Text)
	case archipelago.PartItemName:
		return item(part.Text)
	case archipelago.PartLocationID:
		if id, err := strconv.ParseInt(part.Text, 10, 64); err == nil {
			return location(n.LocationName(part.Player, id))
		}
		return location(part.Text)
	case archipelago.PartColor:
		switch part.Color {
		case "bold":
			return seabird.NewBoldBlock(seabird.NewTextBlock(part.Text))
		case "underline":
			return seabird.NewUnderlineBlock(seabird.NewTextBlock(part.Text))
		}
	}
	// text, location_name, entrance_name, hint_status, and unknown types.
	return seabird.NewTextBlock(part.Text)
}

// renderLifecycle renders Join, Part, Goal, Release, and Collect messages.
// The server sends these as one text part starting with the player's alias;
// the alias is bolded and the client version and tags suffix on Join and
// Part is dropped.
func renderLifecycle(n names, msg archipelago.PrintJSON) *pb.Block {
	var b strings.Builder
	for _, part := range msg.Data {
		b.WriteString(part.Text)
	}
	text := b.String()
	if before, _, ok := strings.Cut(text, " Client("); ok {
		text = before
	}

	alias := n.PlayerName(msg.Slot)
	if rest, ok := strings.CutPrefix(text, alias); ok {
		return seabird.NewContainerBlock(player(alias), seabird.NewTextBlock(rest))
	}
	return seabird.NewTextBlock(text)
}

// renderHint mirrors the server's hint text.
func renderHint(n names, h archipelago.Hint) *pb.Block {
	blocks := []*pb.Block{
		seabird.NewTextBlock("[Hint] "),
		player(n.PlayerName(h.ReceivingPlayer)),
		seabird.NewTextBlock("'s "),
		item(n.ItemName(h.ReceivingPlayer, h.Item)),
		seabird.NewTextBlock(" is at "),
		location(n.LocationName(h.FindingPlayer, h.Location)),
		seabird.NewTextBlock(" in "),
		player(n.PlayerName(h.FindingPlayer)),
		seabird.NewTextBlock("'s World"),
	}
	if h.Entrance != "" {
		blocks = append(blocks, seabird.NewTextBlock(" at "+h.Entrance))
	}
	blocks = append(blocks, seabird.NewTextBlock("."))
	return seabird.NewContainerBlock(blocks...)
}

// renderDeathLink uses the cause when present, which by convention already
// names the player; otherwise it reports the source.
func renderDeathLink(d archipelago.DeathLink) *pb.Block {
	prefix := seabird.NewTextBlock("[DeathLink] ")
	if cause := strings.TrimSpace(d.Cause); cause != "" {
		if rest, ok := strings.CutPrefix(cause, d.Source); ok && d.Source != "" {
			return seabird.NewContainerBlock(prefix, player(d.Source), seabird.NewTextBlock(rest))
		}
		return seabird.NewContainerBlock(prefix, seabird.NewTextBlock(cause))
	}
	return seabird.NewContainerBlock(prefix, player(d.Source), seabird.NewTextBlock(" died."))
}
