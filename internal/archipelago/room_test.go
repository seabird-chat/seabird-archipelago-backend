package archipelago

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRoom(t *testing.T) {
	tests := []struct {
		in      string
		want    Room
		wantErr bool
	}{
		{in: "abc123", want: Room{WebHost: DefaultWebHost, ID: "abc123"}},
		{in: "https://archipelago.gg/room/abc123", want: Room{WebHost: "https://archipelago.gg", ID: "abc123"}},
		{in: "http://localhost:8080/room/abc123/", want: Room{WebHost: "http://localhost:8080", ID: "abc123"}},
		{in: "", wantErr: true},
		{in: "abc/123", wantErr: true},
		{in: "https://archipelago.gg/tracker/abc123", wantErr: true},
		{in: "https://archipelago.gg/room/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseRoom(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRoomStatusAwake(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute).Format(http.TimeFormat)
	stale := now.Add(-2 * time.Hour).Format(http.TimeFormat)

	tests := []struct {
		name   string
		status RoomStatus
		want   bool
	}{
		{"running", RoomStatus{LastPort: 38281, LastActivity: recent, Timeout: 7200 - 1}, true},
		{"timed out", RoomStatus{LastPort: 38281, LastActivity: stale, Timeout: 3600}, false},
		{"never started", RoomStatus{LastPort: 0, LastActivity: recent, Timeout: 7200}, false},
		{"failed to start", RoomStatus{LastPort: -1, LastActivity: recent, Timeout: 7200}, false},
		{"unparseable time", RoomStatus{LastPort: 38281, LastActivity: "yesterday", Timeout: 7200}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Awake(now))
		})
	}
}

func TestRoomStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/room_status/abc123" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"last_port": 38281, "last_activity": "Sat, 14 Nov 2026 12:00:00 GMT", "timeout": 7200, "players": []}`))
	}))
	t.Cleanup(srv.Close)

	room := Room{WebHost: srv.URL, ID: "abc123"}
	status, err := room.Status(context.Background(), srv.Client())
	require.NoError(t, err)
	assert.Equal(t, RoomStatus{LastPort: 38281, LastActivity: "Sat, 14 Nov 2026 12:00:00 GMT", Timeout: 7200}, status)
	assert.Equal(t, "127.0.0.1:38281", room.Addr(status.LastPort))

	_, err = Room{WebHost: srv.URL, ID: "missing"}.Status(context.Background(), srv.Client())
	assert.ErrorContains(t, err, "room not found")
}
