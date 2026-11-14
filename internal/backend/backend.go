// Package backend bridges an Archipelago room into a seabird channel.
package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/belak/x/slogx"
	"github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	"golang.org/x/sync/errgroup"

	"github.com/seabird-chat/seabird-archipelago-backend/internal/archipelago"
)

// BackendType is the seabird backend type, used as the channel URI scheme.
const BackendType = "archipelago"

// serverUserID is the seabird user for messages from the room itself.
// Players are "<team>-<slot>", so it can't collide with one.
const serverUserID = "server"

const (
	ingestRetryDelay = 5 * time.Second
	// The output buffer absorbs bursts like a release while seabird is
	// briefly unavailable; past that, events are dropped.
	eventBufferSize = 256
)

// Config configures a Backend.
type Config struct {
	SeabirdHost  string
	SeabirdToken string
	SeabirdID    string

	// Exactly one of Room and URL is used; URL wins if both are set.
	Room *archipelago.Room
	URL  string

	Slot     string
	Password string

	Channel       string
	CommandPrefix string
	Items         ItemFilter
	DeathLink     bool

	Logger *slog.Logger
}

// Backend relays between one Archipelago room and seabird.
type Backend struct {
	cfg    Config
	logger *slog.Logger
	ingest *seabird.ChatIngestClient
	http   *http.Client
	events chan *pb.ChatEvent
	cache  archipelago.DataPackageCache
	echoes echoTracker

	mu      sync.Mutex
	session *archipelago.Session

	// Owned by the room goroutine.
	hints  hintTracker
	deaths deathTracker
}

// New creates a Backend. It does not connect to anything until Run.
func New(cfg Config) (*Backend, error) {
	if cfg.Room == nil && cfg.URL == "" {
		return nil, errors.New("a room or url is required")
	}
	if cfg.Slot == "" {
		return nil, errors.New("a slot name is required")
	}

	ingest, err := seabird.NewChatIngestClient(cfg.SeabirdHost, cfg.SeabirdToken)
	if err != nil {
		return nil, fmt.Errorf("create seabird ingest client: %w", err)
	}

	return &Backend{
		cfg:    cfg,
		logger: cfg.Logger,
		ingest: ingest,
		http:   &http.Client{Timeout: 30 * time.Second},
		events: make(chan *pb.ChatEvent, eventBufferSize),
	}, nil
}

// Run relays until ctx is cancelled.
func (b *Backend) Run(ctx context.Context) error {
	defer b.ingest.Close()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return b.runIngest(ctx) })
	g.Go(func() error { return b.runRoom(ctx) })

	err := g.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// emit queues an event for seabird, dropping it if the buffer is full so a
// seabird outage can't stall the room connection.
func (b *Backend) emit(e *pb.ChatEvent) {
	select {
	case b.events <- e:
	default:
		b.logger.Warn("dropping event, seabird output buffer full")
	}
}

func (b *Backend) runIngest(ctx context.Context) error {
	for {
		err := b.handleIngest(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b.logger.Warn("seabird ingest stream ended", slogx.Err(err))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ingestRetryDelay):
		}
	}
}

func (b *Backend) handleIngest(ctx context.Context) error {
	stream, err := b.ingest.IngestEvents(BackendType, b.cfg.SeabirdID)
	if err != nil {
		return fmt.Errorf("start ingest: %w", err)
	}
	defer stream.Close()

	// The room is the backend's only channel and exists whether or not the
	// room server is reachable.
	err = stream.Send(&pb.ChatEvent{Inner: &pb.ChatEvent_JoinChannel{
		JoinChannel: &pb.JoinChannelChatEvent{
			ChannelId:   b.cfg.Channel,
			DisplayName: b.cfg.Channel,
		},
	}})
	if err != nil {
		return fmt.Errorf("send join: %w", err)
	}
	b.logger.Info("connected to seabird", slogx.String("channel", b.cfg.Channel))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event := <-b.events:
			if err := stream.Send(event); err != nil {
				return fmt.Errorf("send event: %w", err)
			}

		case req, ok := <-stream.C:
			if !ok {
				return errors.New("stream closed")
			}
			err := b.handleRequest(ctx, req)
			if req.Id == "" {
				if err != nil {
					b.logger.Warn("request failed", slogx.Err(err))
				}
				continue
			}

			reply := &pb.ChatEvent{Id: req.Id, Inner: &pb.ChatEvent_Success{Success: &pb.SuccessChatEvent{}}}
			if err != nil {
				reply.Inner = &pb.ChatEvent_Failed{Failed: &pb.FailedChatEvent{Reason: err.Error()}}
			}
			if err := stream.Send(reply); err != nil {
				return fmt.Errorf("send reply: %w", err)
			}
		}
	}
}

func (b *Backend) handleRequest(ctx context.Context, req *pb.ChatRequest) error {
	switch v := req.Inner.(type) {
	case *pb.ChatRequest_SendMessage:
		return b.say(ctx, v.SendMessage.ChannelId, v.SendMessage.Text)
	case *pb.ChatRequest_PerformAction:
		return b.say(ctx, v.PerformAction.ChannelId, "* "+v.PerformAction.Text)
	case *pb.ChatRequest_SendPrivateMessage, *pb.ChatRequest_PerformPrivateAction:
		return errors.New("archipelago does not support private messages")
	case *pb.ChatRequest_JoinChannel, *pb.ChatRequest_LeaveChannel:
		return errors.New("archipelago backend has a single fixed channel")
	case *pb.ChatRequest_UpdateChannelInfo:
		return errors.New("archipelago rooms have no topic")
	default:
		return fmt.Errorf("unsupported request %T", req.Inner)
	}
}

// sayLines splits text into lines the server will accept as chat. Each
// line is a separate Say, so each is checked on its own: the server parses
// every line for commands with shlex, and leading whitespace, quotes, or
// backslashes don't hide a "!". Seabird must never be able to run !release,
// !collect, and so on, so any such line rejects the whole message.
func sayLines(text string) ([]string, error) {
	var lines []string
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(sanitize(line))
		if line == "" {
			continue
		}
		first := strings.TrimLeftFunc(line, func(r rune) bool {
			return unicode.IsSpace(r) || r == '"' || r == '\'' || r == '\\'
		})
		if strings.HasPrefix(first, "!") {
			return nil, errors.New("lines starting with ! are archipelago server commands and are not sent")
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, errors.New("empty message")
	}
	return lines, nil
}

// sanitize makes a line pass Python's str.isprintable, which the server
// requires: whitespace becomes a plain space and other unprintable runes
// (control and format characters like zero-width joiners) are dropped.
func sanitize(line string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsPrint(r):
			return r
		default:
			return -1
		}
	}, line)
}

func (b *Backend) say(ctx context.Context, channel, text string) error {
	if channel != b.cfg.Channel {
		return fmt.Errorf("unknown channel %q", channel)
	}
	lines, err := sayLines(text)
	if err != nil {
		return err
	}

	b.mu.Lock()
	session := b.session
	b.mu.Unlock()
	if session == nil {
		return errors.New("not connected to the archipelago room")
	}

	for _, line := range lines {
		b.echoes.add(line)
	}
	if err := session.Say(ctx, lines...); err != nil {
		for _, line := range lines {
			b.echoes.take(line)
		}
		return err
	}
	return nil
}
