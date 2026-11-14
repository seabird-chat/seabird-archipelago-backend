package archipelago_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago/aptest"
)

var testGames = map[string]aptest.Game{
	"Archipelago": {Items: map[string]int64{"Nothing": -1}, Locations: map[string]int64{"Cheat Console": -1}},
	"Game A":      {Items: map[string]int64{"Hookshot": 10}, Locations: map[string]int64{"Boss Chest": 40}},
}

func connect(t *testing.T, addr string, cache *archipelago.DataPackageCache) *archipelago.Session {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	conn, err := archipelago.Dial(ctx, addr)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	s, err := archipelago.Handshake(ctx, conn, archipelago.ConnectOptions{
		Slot: "seabird",
		Tags: []string{archipelago.TagTextOnly},
	}, cache)
	require.NoError(t, err)
	return s
}

func TestHandshake(t *testing.T) {
	connected := make(chan map[string]any, 1)
	srv := aptest.NewServer(t, func(p *aptest.Peer) {
		connected <- p.Handshake("seed1", testGames, 2,
			map[int]string{1: "Alice", 2: "seabird"},
			map[int]string{1: "Game A", 2: "Game A"})

		say := p.Expect("Say")
		p.Send(map[string]any{"cmd": "PrintJSON", "type": "Chat", "message": say["text"]})
		p.Wait()
	})

	var cache archipelago.DataPackageCache
	s := connect(t, srv.Addr, &cache)

	pkt := <-connected
	assert.Equal(t, "seabird", pkt["name"])
	assert.Equal(t, "", pkt["game"])
	assert.Equal(t, float64(0), pkt["items_handling"])
	assert.Equal(t, []any{"TextOnly"}, pkt["tags"])

	assert.Equal(t, "seed1", s.Room.SeedName)
	assert.Equal(t, 2, s.Slot)
	assert.Equal(t, "Alice", s.PlayerName(1))
	assert.Equal(t, "Archipelago", s.PlayerName(0))
	assert.Equal(t, "Player 9", s.PlayerName(9))
	assert.Equal(t, "Hookshot", s.ItemName(1, 10))
	assert.Equal(t, "Boss Chest", s.LocationName(1, 40))
	assert.Equal(t, "Cheat Console", s.LocationName(0, -1))
	assert.Equal(t, "Unknown item (ID: 11)", s.ItemName(1, 11))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, s.Say(ctx, "hello"))

	packets, err := s.Read(ctx)
	require.NoError(t, err)
	require.Len(t, packets, 1)
	var msg archipelago.PrintJSON
	require.NoError(t, packets[0].Decode(&msg))
	assert.Equal(t, "hello", msg.Message)
}

func TestHandshakeRefused(t *testing.T) {
	srv := aptest.NewServer(t, func(p *aptest.Peer) {
		p.Send(map[string]any{"cmd": "RoomInfo", "seed_name": "seed1"})
		p.Expect("Connect")
		p.Send(map[string]any{"cmd": "ConnectionRefused", "errors": []string{"InvalidSlot"}})
		p.Wait()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	conn, err := archipelago.Dial(ctx, srv.Addr)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	_, err = archipelago.Handshake(ctx, conn, archipelago.ConnectOptions{Slot: "nobody"}, &archipelago.DataPackageCache{})
	var refused *archipelago.RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, []string{"InvalidSlot"}, refused.Errors)
}

func TestRoomUpdateAliases(t *testing.T) {
	srv := aptest.NewServer(t, func(p *aptest.Peer) {
		p.Handshake("seed1", testGames, 2,
			map[int]string{1: "Alice", 2: "seabird"},
			map[int]string{1: "Game A", 2: "Game A"})
		p.Send(map[string]any{"cmd": "RoomUpdate", "players": []map[string]any{
			{"team": 0, "slot": 1, "alias": "Alicia", "name": "Alice"},
		}})
		p.Wait()
	})

	s := connect(t, srv.Addr, &archipelago.DataPackageCache{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	_, err := s.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Alicia", s.PlayerName(1))
}
