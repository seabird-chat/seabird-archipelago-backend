package archipelago

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultWebHost is used for rooms given as a bare ID.
const DefaultWebHost = "https://archipelago.gg"

// Room is a room on an Archipelago web host. Hosted rooms shut down when
// idle and may come back on a different port, so the port has to be looked
// up before each connection.
type Room struct {
	WebHost string
	ID      string
}

// ParseRoom accepts a room ID or a room URL like
// https://archipelago.gg/room/<id>.
func ParseRoom(s string) (Room, error) {
	if !strings.Contains(s, "://") {
		if s == "" || strings.Contains(s, "/") {
			return Room{}, fmt.Errorf("invalid room id %q", s)
		}
		return Room{WebHost: DefaultWebHost, ID: s}, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return Room{}, fmt.Errorf("parse room url: %w", err)
	}
	id, ok := strings.CutPrefix(strings.TrimSuffix(u.Path, "/"), "/room/")
	if !ok || id == "" || strings.Contains(id, "/") {
		return Room{}, fmt.Errorf("room url %q is not of the form <host>/room/<id>", s)
	}
	return Room{WebHost: u.Scheme + "://" + u.Host, ID: id}, nil
}

// RoomStatus is the subset of /api/room_status used to find the server.
type RoomStatus struct {
	LastPort     int    `json:"last_port"`
	LastActivity string `json:"last_activity"`
	Timeout      int    `json:"timeout"`
}

// Awake reports whether the room's server is likely running. The web host
// backdates last_activity past the timeout when a room shuts down, and a
// room that has never started or failed to start has no usable port.
func (s RoomStatus) Awake(now time.Time) bool {
	if s.LastPort <= 0 {
		return false
	}
	last, err := http.ParseTime(s.LastActivity)
	if err != nil {
		// Unknown format; let the connection attempt decide.
		return true
	}
	return last.Add(time.Duration(s.Timeout) * time.Second).After(now)
}

// Status fetches the room's current status. It never wakes a sleeping room.
func (r Room) Status(ctx context.Context, client *http.Client) (RoomStatus, error) {
	statusURL := r.WebHost + "/api/room_status/" + url.PathEscape(r.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return RoomStatus{}, fmt.Errorf("build room status request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return RoomStatus{}, fmt.Errorf("get room status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return RoomStatus{}, errors.New("room not found")
	}
	if resp.StatusCode != http.StatusOK {
		return RoomStatus{}, fmt.Errorf("get room status: unexpected status %s", resp.Status)
	}

	var status RoomStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return RoomStatus{}, fmt.Errorf("decode room status: %w", err)
	}
	return status, nil
}

// Addr returns the server address for the given port, without a scheme so
// Dial can pick wss or ws.
func (r Room) Addr(port int) string {
	host := r.WebHost
	if u, err := url.Parse(r.WebHost); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
