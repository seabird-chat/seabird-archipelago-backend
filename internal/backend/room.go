package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/belak/x/slogx"
	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
)

const (
	dialTimeout = 30 * time.Second
	// Long enough to download data packages for a large multiworld.
	handshakeTimeout = 2 * time.Minute
	minBackoff       = 5 * time.Second
	maxBackoff       = 5 * time.Minute
	// A session that lasted this long counts as healthy and resets backoff.
	healthySession = time.Minute
	// How often to check on a room that is asleep.
	asleepPoll = time.Minute
)

var errRoomAsleep = errors.New("room is asleep")

// roomState is per-connection state for the room goroutine.
type roomState struct {
	session *archipelago.Session
	// The server announces our own Join to us; it's skipped once.
	ownJoinSeen bool
	// Only the start and end of a countdown are announced, not every tick.
	countdownActive bool
}

func (b *Backend) runRoom(ctx context.Context) error {
	backoff := minBackoff
	asleep := false

	for {
		start := time.Now()
		err := b.connectRoom(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		wait := backoff
		switch {
		case errors.Is(err, errRoomAsleep):
			if !asleep {
				b.logger.Info("room is asleep, waiting for someone to wake it")
			}
			asleep = true
			wait = asleepPoll
		default:
			asleep = false
			if time.Since(start) >= healthySession {
				backoff = minBackoff
				wait = backoff
			}
			b.logger.Warn("room connection ended", slogx.Err(err), slogx.Duration("retry_in", wait))
			backoff = min(backoff*2, maxBackoff)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (b *Backend) roomAddr(ctx context.Context) (string, error) {
	if b.cfg.URL != "" {
		return b.cfg.URL, nil
	}

	status, err := b.cfg.Room.Status(ctx, b.http)
	if err != nil {
		return "", err
	}
	if !status.Awake(time.Now()) {
		return "", errRoomAsleep
	}
	return b.cfg.Room.Addr(status.LastPort), nil
}

func (b *Backend) connectRoom(ctx context.Context) error {
	addr, err := b.roomAddr(ctx)
	if err != nil {
		return err
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	conn, err := archipelago.Dial(dialCtx, addr)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close()

	tags := []string{archipelago.TagTextOnly}
	if b.cfg.DeathLink {
		tags = append(tags, archipelago.TagDeathLink)
	}

	hsCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	session, err := archipelago.Handshake(hsCtx, conn, archipelago.ConnectOptions{
		Slot:     b.cfg.Slot,
		Password: b.cfg.Password,
		UUID:     "seabird-archipelago-backend-" + b.cfg.SeabirdID,
		Tags:     tags,
	}, &b.cache)
	cancel()
	if err != nil {
		return fmt.Errorf("handshake with %s: %w", addr, err)
	}

	b.hints.start(session.Room.SeedName)
	if err := session.WatchHints(ctx); err != nil {
		return fmt.Errorf("watch hints: %w", err)
	}

	b.mu.Lock()
	b.session = session
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.session = nil
		b.mu.Unlock()
		b.echoes.clear()
	}()

	b.logger.Info("connected to archipelago room",
		slogx.String("addr", addr),
		slogx.String("seed", session.Room.SeedName),
		slogx.Int("team", session.Team),
		slogx.Int("slot", session.Slot))

	state := &roomState{session: session}
	for {
		packets, err := session.Read(ctx)
		if err != nil {
			return err
		}
		for _, p := range packets {
			if err := b.handlePacket(state, p); err != nil {
				b.logger.Warn("failed to handle packet", slogx.String("cmd", p.Cmd), slogx.Err(err))
			}
		}
	}
}

func (b *Backend) handlePacket(state *roomState, p archipelago.Packet) error {
	b.logger.Debug("packet", slogx.String("cmd", p.Cmd), slogx.Int("bytes", len(p.Raw)))

	switch p.Cmd {
	case "PrintJSON":
		var msg archipelago.PrintJSON
		if err := p.Decode(&msg); err != nil {
			return err
		}
		b.handlePrint(state, msg)

	case "Bounced":
		var bounce archipelago.Bounced
		if err := p.Decode(&bounce); err != nil {
			return err
		}
		return b.handleBounce(bounce)

	case "Retrieved":
		var r archipelago.Retrieved
		if err := p.Decode(&r); err != nil {
			return err
		}
		return b.handleRetrieved(state, r)

	case "SetReply":
		var r archipelago.SetReply
		if err := p.Decode(&r); err != nil {
			return err
		}
		return b.handleSetReply(state, r)
	}
	return nil
}

func (b *Backend) handlePrint(state *roomState, msg archipelago.PrintJSON) {
	s := state.session

	switch msg.Type {
	case archipelago.PrintChat:
		if !b.cfg.Chat {
			return
		}
		if msg.Team == s.Team && msg.Slot == s.Slot && b.echoes.take(msg.Message) {
			return
		}
		b.emitChat(chatUser(s, msg), msg.Message)

	case archipelago.PrintItemSend, archipelago.PrintItemCheat:
		if b.cfg.Items.Allows(msg.Item.Flags) {
			b.emitServer(renderParts(s, msg.Data))
		}

	case archipelago.PrintJoin:
		if !state.ownJoinSeen && msg.Slot == s.Slot && slices.Contains(msg.Tags, archipelago.TagTextOnly) {
			state.ownJoinSeen = true
			return
		}
		b.emitServer(renderLifecycle(s, msg))

	case archipelago.PrintPart, archipelago.PrintGoal, archipelago.PrintRelease, archipelago.PrintCollect:
		b.emitServer(renderLifecycle(s, msg))

	case archipelago.PrintCountdown:
		if msg.Countdown == 0 {
			state.countdownActive = false
			b.emitServer(renderParts(s, msg.Data))
			return
		}
		if !state.countdownActive {
			state.countdownActive = true
			b.emitServer(renderParts(s, msg.Data))
		}

	case archipelago.PrintHint:
		// Hints are announced from data storage instead, which sees every
		// slot's hints rather than only those involving ours.

	case archipelago.PrintTutorial, archipelago.PrintTagsChanged,
		archipelago.PrintCommandResult, archipelago.PrintAdminCommandResult:
		// Directed at this client or noise.

	default:
		// ServerChat and untyped broadcasts such as a team completing.
		b.emitServer(renderParts(s, msg.Data))
	}
}

func (b *Backend) handleBounce(bounce archipelago.Bounced) error {
	if !b.cfg.DeathLink || !slices.Contains(bounce.Tags, archipelago.TagDeathLink) {
		return nil
	}

	var death archipelago.DeathLink
	if err := json.Unmarshal(bounce.Data, &death); err != nil {
		return fmt.Errorf("decode deathlink: %w", err)
	}
	if b.deaths.isNew(death) {
		b.emitServer(renderDeathLink(death))
	}
	return nil
}

func (b *Backend) handleRetrieved(state *roomState, r archipelago.Retrieved) error {
	var lists [][]archipelago.Hint
	for key, raw := range r.Keys {
		if !strings.HasPrefix(key, "_read_hints_") {
			continue
		}
		var hints []archipelago.Hint
		if err := json.Unmarshal(raw, &hints); err != nil {
			return fmt.Errorf("decode %s: %w", key, err)
		}
		lists = append(lists, hints)
	}
	if lists == nil {
		return nil
	}
	b.announceHints(state, b.hints.snapshot(lists))
	return nil
}

func (b *Backend) handleSetReply(state *roomState, r archipelago.SetReply) error {
	if !strings.HasPrefix(r.Key, "_read_hints_") {
		return nil
	}
	var hints []archipelago.Hint
	if err := json.Unmarshal(r.Value, &hints); err != nil {
		return fmt.Errorf("decode %s: %w", r.Key, err)
	}
	b.announceHints(state, b.hints.update(hints))
	return nil
}

func (b *Backend) announceHints(state *roomState, hints []archipelago.Hint) {
	for _, h := range hints {
		if b.cfg.Items.Allows(h.ItemFlags) {
			b.emitServer(renderHint(state.session, h))
		}
	}
}

func (b *Backend) source(user *pb.User) *pb.ChannelSource {
	return &pb.ChannelSource{ChannelId: b.cfg.Channel, User: user}
}

// emitServer posts a message from the synthetic Archipelago user.
func (b *Backend) emitServer(block *pb.Block) {
	b.emit(&pb.ChatEvent{Inner: &pb.ChatEvent_Message{Message: &pb.MessageEvent{
		Source:    b.source(&pb.User{Id: serverUserID, DisplayName: "Archipelago"}),
		Text:      block.Plain,
		RootBlock: block,
	}}})
}

// chatUser identifies a chat sender. Chat is broadcast to every team, but
// aliases are only tracked for ours; other teams' names come from the
// "Alias: message" text.
func chatUser(s *archipelago.Session, msg archipelago.PrintJSON) *pb.User {
	id := strconv.Itoa(msg.Team) + "-" + strconv.Itoa(msg.Slot)
	if msg.Team == s.Team {
		return &pb.User{Id: id, DisplayName: s.PlayerName(msg.Slot)}
	}

	var text strings.Builder
	for _, part := range msg.Data {
		text.WriteString(part.Text)
	}
	name, ok := strings.CutSuffix(text.String(), ": "+msg.Message)
	if !ok || name == "" {
		name = fmt.Sprintf("Team %d Player %d", msg.Team+1, msg.Slot)
	}
	return &pb.User{Id: id, DisplayName: name}
}

// emitChat posts a player's chat, as a command if it has the prefix.
func (b *Backend) emitChat(user *pb.User, text string) {
	source := b.source(user)

	if b.cfg.CommandPrefix != "" {
		if rest, ok := strings.CutPrefix(text, b.cfg.CommandPrefix); ok && rest != "" {
			cmd, arg, _ := strings.Cut(rest, " ")
			b.emit(&pb.ChatEvent{Inner: &pb.ChatEvent_Command{Command: &pb.CommandEvent{
				Source:  source,
				Command: cmd,
				Arg:     strings.TrimSpace(arg),
			}}})
			return
		}
	}

	b.emit(&pb.ChatEvent{Inner: &pb.ChatEvent_Message{Message: &pb.MessageEvent{
		Source:    source,
		Text:      text,
		RootBlock: seabird.NewTextBlock(text),
	}}})
}
