// Package aptest provides a scripted fake Archipelago server for tests.
package aptest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Server accepts one connection and runs a script against it.
type Server struct {
	// Addr is the server address with a ws:// scheme.
	Addr string
}

// NewServer starts a server that runs script for each connection. The
// script runs on the server goroutine; it should report problems with
// t.Errorf rather than t.Fatal.
func NewServer(t *testing.T, script func(p *Peer)) *Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer ws.CloseNow()
		ws.SetReadLimit(-1)

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		script(&Peer{t: t, ctx: ctx, ws: ws})
	}))
	t.Cleanup(srv.Close)

	return &Server{Addr: "ws://" + strings.TrimPrefix(srv.URL, "http://")}
}

// Peer is the server side of a client connection.
type Peer struct {
	t       *testing.T
	ctx     context.Context
	ws      *websocket.Conn
	pending []map[string]any
}

// Send writes packets as one frame.
func (p *Peer) Send(packets ...any) {
	data, err := json.Marshal(packets)
	if err != nil {
		p.t.Errorf("marshal: %v", err)
		return
	}
	if err := p.ws.Write(p.ctx, websocket.MessageText, data); err != nil {
		p.t.Errorf("write: %v", err)
	}
}

// Expect reads packets until one with the given cmd arrives and returns
// it. Earlier packets are discarded.
func (p *Peer) Expect(cmd string) map[string]any {
	for {
		for len(p.pending) > 0 {
			packet := p.pending[0]
			p.pending = p.pending[1:]
			if packet["cmd"] == cmd {
				return packet
			}
		}

		_, data, err := p.ws.Read(p.ctx)
		if err != nil {
			p.t.Errorf("waiting for %s: %v", cmd, err)
			return nil
		}
		if err := json.Unmarshal(data, &p.pending); err != nil {
			p.t.Errorf("decode frame: %v", err)
			return nil
		}
	}
}

// Wait blocks until the client disconnects or the script times out.
func (p *Peer) Wait() {
	for {
		if _, _, err := p.ws.Read(p.ctx); err != nil {
			return
		}
	}
}

// Game is test data for one game in the data package.
type Game struct {
	Items     map[string]int64
	Locations map[string]int64
}

// Handshake plays the server side of a successful connection: RoomInfo,
// the data package if requested, and Connected. It returns the Connect
// packet the client sent.
func (p *Peer) Handshake(seed string, games map[string]Game, slot int, players map[int]string, slotGames map[int]string) map[string]any {
	names := make([]string, 0, len(games))
	checksums := map[string]string{}
	for name := range games {
		names = append(names, name)
		checksums[name] = "sum-" + name
	}
	p.Send(map[string]any{
		"cmd":                   "RoomInfo",
		"seed_name":             seed,
		"games":                 names,
		"datapackage_checksums": checksums,
	})

	req := p.Expect("GetDataPackage")
	if req == nil {
		return nil
	}
	pkg := map[string]any{}
	for _, name := range req["games"].([]any) {
		g := games[name.(string)]
		pkg[name.(string)] = map[string]any{
			"item_name_to_id":     g.Items,
			"location_name_to_id": g.Locations,
			"checksum":            checksums[name.(string)],
		}
	}
	p.Send(map[string]any{"cmd": "DataPackage", "data": map[string]any{"games": pkg}})

	connect := p.Expect("Connect")
	if connect == nil {
		return nil
	}

	var netPlayers []map[string]any
	slotInfo := map[string]any{}
	for s, name := range players {
		netPlayers = append(netPlayers, map[string]any{"team": 0, "slot": s, "alias": name, "name": name})
		slotInfo[jsonInt(s)] = map[string]any{"name": name, "game": slotGames[s], "type": 1}
	}
	p.Send(map[string]any{
		"cmd":       "Connected",
		"team":      0,
		"slot":      slot,
		"players":   netPlayers,
		"slot_info": slotInfo,
	})
	return connect
}

func jsonInt(i int) string {
	data, _ := json.Marshal(i)
	return string(data)
}
