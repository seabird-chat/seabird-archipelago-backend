package backend

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/seabird-chat/seabird-go/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago/aptest"
)

var testGames = map[string]aptest.Game{
	"Archipelago": {},
	"Game A": {
		Items:     map[string]int64{"Hookshot": 10, "Rupee": 11},
		Locations: map[string]int64{"Boss Chest": 40, "Pot": 41},
	},
}

func printJSON(typ string, extra map[string]any) map[string]any {
	p := map[string]any{"cmd": "PrintJSON", "type": typ}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func itemSend(item, flags int) map[string]any {
	return printJSON("ItemSend", map[string]any{
		"receiving": 2,
		"item":      map[string]any{"item": item, "location": 40, "player": 1, "flags": flags},
		"data": []map[string]any{
			{"type": "player_id", "text": "1"},
			{"text": " sent "},
			{"type": "item_id", "text": strconv.Itoa(item), "player": 2, "flags": flags},
			{"text": " to "},
			{"type": "player_id", "text": "2"},
			{"text": " ("},
			{"type": "location_id", "text": "40", "player": 1},
			{"text": ")"},
		},
	})
}

func chat(slot int, alias, message string) map[string]any {
	return printJSON("Chat", map[string]any{
		"team": 0, "slot": slot, "message": message,
		"data": []map[string]any{{"text": alias + ": " + message}},
	})
}

func hint(finder, receiver, item, location int, found bool) map[string]any {
	return map[string]any{
		"receiving_player": receiver, "finding_player": finder,
		"item": item, "location": location, "found": found,
		"entrance": "", "item_flags": 1, "status": 0, "class": "Hint",
	}
}

func TestConnectRoom(t *testing.T) {
	sayDone := make(chan struct{})

	srv := aptest.NewServer(t, func(p *aptest.Peer) {
		connect := p.Handshake("seed1", testGames, 3,
			map[int]string{1: "Alice", 2: "Bob", 3: "seabird"},
			map[int]string{1: "Game A", 2: "Game A", 3: "Game A"})
		assert.Equal(t, []any{"TextOnly", "DeathLink"}, connect["tags"])

		get := p.Expect("Get")
		assert.Equal(t, []any{"_read_hints_0_1", "_read_hints_0_2", "_read_hints_0_3"}, get["keys"])
		p.Expect("SetNotify")

		// Existing hints are recorded silently.
		p.Send(map[string]any{"cmd": "Retrieved", "keys": map[string]any{
			"_read_hints_0_1": []any{hint(1, 2, 10, 40, false)},
			"_read_hints_0_2": []any{hint(1, 2, 10, 40, false)},
			"_read_hints_0_3": []any{},
		}})

		p.Send(
			printJSON("Join", map[string]any{"team": 0, "slot": 3, "tags": []string{"TextOnly"},
				"data": []map[string]any{{"text": "seabird (Team #1) viewing Game A has joined."}}}),
			printJSON("Tutorial", map[string]any{"data": []map[string]any{{"text": "Now that you are connected..."}}}),
			itemSend(10, 1),
			itemSend(11, 0), // filtered out
			chat(1, "Alice", ".roll 2d6"),
			chat(1, "Alice", "hi"),
			printJSON("Hint", map[string]any{"data": []map[string]any{{"text": "[Hint]: ignored"}}}),
		)
		p.Send(map[string]any{"cmd": "SetReply", "key": "_read_hints_0_2", "value": []any{
			hint(1, 2, 10, 40, false),
			hint(2, 2, 10, 41, false),
			hint(1, 2, 11, 41, true), // already found
		}})
		death := map[string]any{"cmd": "Bounced", "tags": []string{"DeathLink"},
			"data": map[string]any{"time": 1.5, "source": "Bob", "cause": ""}}
		p.Send(death, death)
		p.Send(
			printJSON("Countdown", map[string]any{"countdown": 3, "data": []map[string]any{{"text": "[Server]: Starting countdown of 3s"}}}),
			printJSON("Countdown", map[string]any{"countdown": 3, "data": []map[string]any{{"text": "[Server]: 3"}}}),
			printJSON("Countdown", map[string]any{"countdown": 2, "data": []map[string]any{{"text": "[Server]: 2"}}}),
			printJSON("Countdown", map[string]any{"countdown": 0, "data": []map[string]any{{"text": "[Server]: GO"}}}),
		)

		say := p.Expect("Say")
		assert.Equal(t, "hello room", say["text"])
		close(sayDone)
		p.Send(
			chat(3, "seabird", "hello room"), // echo, dropped
			printJSON("ServerChat", map[string]any{"data": []map[string]any{{"text": "[Server]: done"}}}),
		)
		p.Wait()
	})

	b := &Backend{
		cfg: Config{
			SeabirdID:     "test",
			URL:           srv.Addr,
			Slot:          "seabird",
			Channel:       "room",
			CommandPrefix: ".",
			Items:         ItemProgression,
			DeathLink:     true,
			Chat:          true,
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		http:   http.DefaultClient,
		events: make(chan *pb.ChatEvent, 64),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	go func() { _ = b.connectRoom(ctx) }()

	next := func() *pb.ChatEvent {
		t.Helper()
		select {
		case e := <-b.events:
			return e
		case <-ctx.Done():
			t.Fatal("timed out waiting for event")
			return nil
		}
	}
	serverMessage := func(want string) {
		t.Helper()
		msg := next().GetMessage()
		require.NotNil(t, msg)
		assert.Equal(t, serverUserID, msg.Source.User.Id)
		assert.Equal(t, "room", msg.Source.ChannelId)
		assert.Equal(t, want, msg.Text)
	}

	serverMessage("Alice sent Hookshot to Bob (Boss Chest)")

	cmd := next().GetCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "roll", cmd.Command)
	assert.Equal(t, "2d6", cmd.Arg)
	assert.Equal(t, &pb.User{Id: "0-1", DisplayName: "Alice"}, cmd.Source.User)

	msg := next().GetMessage()
	require.NotNil(t, msg)
	assert.Equal(t, "hi", msg.Text)
	assert.Equal(t, "0-1", msg.Source.User.Id)

	serverMessage("[Hint] Bob's Hookshot is at Pot in Bob's World.")
	serverMessage("[DeathLink] Bob died.")
	serverMessage("[Server]: Starting countdown of 3s")
	serverMessage("[Server]: GO")

	require.NoError(t, b.say(ctx, "room", "hello room"))
	<-sayDone
	serverMessage("[Server]: done")

	assert.ErrorContains(t, b.say(ctx, "room", "!release"), "server commands")
	assert.ErrorContains(t, b.say(ctx, "elsewhere", "hi"), "unknown channel")
}

func TestChatDisabled(t *testing.T) {
	srv := aptest.NewServer(t, func(p *aptest.Peer) {
		p.Handshake("seed1", testGames, 3,
			map[int]string{1: "Alice", 2: "Bob", 3: "seabird"},
			map[int]string{1: "Game A", 2: "Game A", 3: "Game A"})
		p.Expect("SetNotify")
		p.Send(chat(1, "Alice", ".roll 2d6"), chat(1, "Alice", "hi"), itemSend(10, 1))
		p.Wait()
	})

	b := &Backend{
		cfg:    Config{URL: srv.Addr, Slot: "seabird", Channel: "room", CommandPrefix: ".", Items: ItemAll},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		http:   http.DefaultClient,
		events: make(chan *pb.ChatEvent, 64),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	go func() { _ = b.connectRoom(ctx) }()

	select {
	case e := <-b.events:
		msg := e.GetMessage()
		require.NotNil(t, msg)
		assert.Equal(t, "Alice sent Hookshot to Bob (Boss Chest)", msg.Text, "chat is skipped")
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}

	assert.ErrorContains(t, b.say(ctx, "room", "hello"), "chat is disabled")
}

func TestHintTrackerReconnect(t *testing.T) {
	var tr hintTracker
	old := archipelago.Hint{FindingPlayer: 1, Location: 40}
	missed := archipelago.Hint{FindingPlayer: 2, Location: 41}

	tr.start("seed1")
	assert.Empty(t, tr.snapshot([][]archipelago.Hint{{old}}), "first snapshot is silent")

	// Same seed after a reconnect: hints made while away are announced.
	tr.start("seed1")
	assert.Equal(t, []archipelago.Hint{missed}, tr.snapshot([][]archipelago.Hint{{old, missed}}))

	// A new seed starts silent again.
	tr.start("seed2")
	assert.Empty(t, tr.snapshot([][]archipelago.Hint{{old, missed}}))
}
