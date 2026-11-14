package archipelago

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// clientVersion is advertised in Connect. Version checks are skipped for
// TextOnly clients with no game, so this only needs to be plausible.
var clientVersion = NetworkVersion{Major: 0, Minor: 6, Build: 0, Class: "Version"}

// serverSlot is slot 0, which the protocol reserves for the server itself.
const serverSlot = 0

// ConnectOptions describe how to authenticate to a room.
type ConnectOptions struct {
	Slot     string
	Password string
	UUID     string
	Tags     []string
}

// RefusedError is returned when the server rejects Connect.
type RefusedError struct {
	Errors []string
}

func (e *RefusedError) Error() string {
	if len(e.Errors) == 0 {
		return "connection refused"
	}
	return "connection refused: " + strings.Join(e.Errors, ", ")
}

type gameNames struct {
	checksum  string
	items     map[int64]string
	locations map[int64]string
}

func newGameNames(data GameData) *gameNames {
	g := &gameNames{
		checksum:  data.Checksum,
		items:     make(map[int64]string, len(data.ItemNameToID)),
		locations: make(map[int64]string, len(data.LocationNameToID)),
	}
	for name, id := range data.ItemNameToID {
		g.items[id] = name
	}
	for name, id := range data.LocationNameToID {
		g.locations[id] = name
	}
	return g
}

// DataPackageCache keeps id to name mappings across reconnects so each game's
// data package is only fetched again when its checksum changes. The zero
// value is ready to use.
type DataPackageCache struct {
	mu    sync.Mutex
	games map[string]*gameNames
}

// missing returns the games in info that aren't cached at their current
// checksum. Servers that don't send checksums only get fetched once per game.
func (c *DataPackageCache) missing(info RoomInfo) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var out []string
	for _, game := range info.Games {
		cached, ok := c.games[game]
		want := info.DataPackageChecksums[game]
		if !ok || (want != "" && cached.checksum != want) {
			out = append(out, game)
		}
	}
	return out
}

func (c *DataPackageCache) store(pkg DataPackage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.games == nil {
		c.games = make(map[string]*gameNames)
	}
	for game, data := range pkg.Data.Games {
		c.games[game] = newGameNames(data)
	}
}

func (c *DataPackageCache) snapshot(games []string) map[string]*gameNames {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make(map[string]*gameNames, len(games))
	for _, game := range games {
		if g, ok := c.games[game]; ok {
			out[game] = g
		}
	}
	return out
}

// Session is an authenticated connection to a room.
type Session struct {
	conn *Conn

	Room RoomInfo
	Team int
	Slot int

	games map[string]*gameNames
	slots map[int]NetworkSlot

	mu      sync.RWMutex
	players map[int]NetworkPlayer
}

// Handshake waits for RoomInfo, fetches any missing data packages, and
// connects to the room. The caller should bound ctx; a server that never
// answers would otherwise block forever.
func Handshake(ctx context.Context, conn *Conn, opts ConnectOptions, cache *DataPackageCache) (*Session, error) {
	var info RoomInfo
	if err := readUntil(ctx, conn, "RoomInfo", &info); err != nil {
		return nil, err
	}

	if missing := cache.missing(info); len(missing) > 0 {
		err := conn.Send(ctx, getDataPackagePacket{Cmd: "GetDataPackage", Games: missing})
		if err != nil {
			return nil, err
		}
		var pkg DataPackage
		if err := readUntil(ctx, conn, "DataPackage", &pkg); err != nil {
			return nil, err
		}
		cache.store(pkg)
	}

	err := conn.Send(ctx, connectPacket{
		Cmd:      "Connect",
		Password: opts.Password,
		Name:     opts.Slot,
		UUID:     opts.UUID,
		Version:  clientVersion,
		Tags:     opts.Tags,
	})
	if err != nil {
		return nil, err
	}

	var connected Connected
	for connected.Slot == 0 {
		packets, err := conn.Read(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range packets {
			switch p.Cmd {
			case "ConnectionRefused":
				var refused ConnectionRefused
				if err := p.Decode(&refused); err != nil {
					return nil, fmt.Errorf("decode ConnectionRefused: %w", err)
				}
				return nil, &RefusedError{Errors: refused.Errors}
			case "Connected":
				if err := p.Decode(&connected); err != nil {
					return nil, fmt.Errorf("decode Connected: %w", err)
				}
			}
		}
	}

	s := &Session{
		conn:    conn,
		Room:    info,
		Team:    connected.Team,
		Slot:    connected.Slot,
		games:   cache.snapshot(info.Games),
		slots:   connected.SlotInfo,
		players: make(map[int]NetworkPlayer),
	}
	s.setPlayers(connected.Players)
	return s, nil
}

func readUntil(ctx context.Context, conn *Conn, cmd string, v any) error {
	for {
		packets, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		for _, p := range packets {
			if p.Cmd != cmd {
				continue
			}
			if err := p.Decode(v); err != nil {
				return fmt.Errorf("decode %s: %w", cmd, err)
			}
			return nil
		}
	}
}

// Read returns the next frame's packets. Alias changes from RoomUpdate are
// applied before returning.
func (s *Session) Read(ctx context.Context) ([]Packet, error) {
	packets, err := s.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range packets {
		if p.Cmd != "RoomUpdate" {
			continue
		}
		var update RoomUpdate
		if err := p.Decode(&update); err != nil {
			return nil, fmt.Errorf("decode RoomUpdate: %w", err)
		}
		if update.Players != nil {
			s.setPlayers(update.Players)
		}
	}
	return packets, nil
}

func (s *Session) setPlayers(players []NetworkPlayer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range players {
		if p.Team == s.Team {
			s.players[p.Slot] = p
		}
	}
}

// Say sends one chat message per line, in a single frame. The server
// rejects lines that aren't printable in Python's sense, newlines included.
func (s *Session) Say(ctx context.Context, lines ...string) error {
	if len(lines) == 0 {
		return errors.New("empty message")
	}
	packets := make([]any, 0, len(lines))
	for _, line := range lines {
		packets = append(packets, sayPacket{Cmd: "Say", Text: line})
	}
	return s.conn.Send(ctx, packets...)
}

// HintsKey is the read-only data storage key holding a slot's hints.
func HintsKey(team, slot int) string {
	return fmt.Sprintf("_read_hints_%d_%d", team, slot)
}

// WatchHints requests every slot's hints on this team and subscribes to
// changes. The server answers with a Retrieved packet, then SetReply packets
// as hints are added. Hint PrintJSON messages only reach the slots involved,
// so this is the only way to see every hint.
func (s *Session) WatchHints(ctx context.Context) error {
	keys := make([]string, 0, len(s.slots))
	for _, slot := range slices.Sorted(maps.Keys(s.slots)) {
		keys = append(keys, HintsKey(s.Team, slot))
	}
	return s.conn.Send(ctx,
		keysPacket{Cmd: "Get", Keys: keys},
		keysPacket{Cmd: "SetNotify", Keys: keys},
	)
}

// Close closes the underlying connection.
func (s *Session) Close() error {
	return s.conn.Close()
}

// PlayerName returns a slot's current alias.
func (s *Session) PlayerName(slot int) string {
	if slot == serverSlot {
		return "Archipelago"
	}

	s.mu.RLock()
	p, ok := s.players[slot]
	s.mu.RUnlock()
	if ok && p.Alias != "" {
		return p.Alias
	}
	if info, ok := s.slots[slot]; ok && info.Name != "" {
		return info.Name
	}
	return fmt.Sprintf("Player %d", slot)
}

func (s *Session) game(slot int) *gameNames {
	game := "Archipelago"
	if slot != serverSlot {
		game = s.slots[slot].Game
	}
	return s.games[game]
}

// ItemName resolves an item id in the given slot's game.
func (s *Session) ItemName(slot int, id int64) string {
	if g := s.game(slot); g != nil {
		if name, ok := g.items[id]; ok {
			return name
		}
	}
	return fmt.Sprintf("Unknown item (ID: %d)", id)
}

// LocationName resolves a location id in the given slot's game.
func (s *Session) LocationName(slot int, id int64) string {
	if g := s.game(slot); g != nil {
		if name, ok := g.locations[id]; ok {
			return name
		}
	}
	return fmt.Sprintf("Unknown location (ID: %d)", id)
}
