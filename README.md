# seabird-archipelago-backend

Or: It's dangerous to play alone, take this chat bridge

A seabird chat backend for [Archipelago](https://archipelago.gg) multiworld
rooms. It connects to one room and exposes it as a single seabird channel.
Item sends, hints, deathlinks, goals, and player chat show up in that
channel, and seabird can send chat into the room. To watch several rooms,
run one instance per room, each with its own `SEABIRD_ID`.

Seabird should never be able to change anyone's game. To keep it that way,
the backend connects as a `TextOnly` client. It never sends locations,
items, or deathlinks, and it refuses any outgoing line that the room server
would treat as an `!` command, such as `!release` or `!collect`.

## What gets relayed

Player chat arrives as messages from a seabird user named after the slot.
Chat that starts with the command prefix (`.` by default) becomes a seabird
command, since `!` already belongs to the Archipelago server.

Everything else comes from a user named `Archipelago`. That covers item
sends (filtered by item class), hints, deathlinks, joins and parts, goals,
releases, collects, and the start and end of countdowns. Hints are read
from the server's data storage rather than from chat messages, because the
server only sends hint messages to the players involved.

Messages from seabird go into the room as chat from the configured slot.
Multi-line messages are sent as one chat line each. Private messages,
channel changes, and topics are not supported. Setting
`ARCHIPELAGO_CHAT=false` turns chat off in both directions and leaves only
the game events.

The item filter works on Archipelago's item classes. `progression_skip_balancing`
is progression that the generator's progression balancing step is told to
leave in place, usually because there are lots of them (currency, shards)
or because they should stay spread out (some goal items). Filtering it out
cuts most of the noise from progression announcements without hiding the
items that unlock things.

## Finding the room

Rooms on archipelago.gg shut down when nobody is playing and can come back
on a different port. With `ARCHIPELAGO_ROOM` set, the backend asks the
web host's room status API for the current port before each connection
attempt. While the room is asleep it checks about once a minute.

It never wakes a sleeping room itself. A chat-only connection doesn't count
as activity, so a room that the backend woke would just shut down again,
and an automatic reconnect would keep starting it up for nobody. Opening
the room page wakes it, and the backend connects once it's up.

For a self-hosted server with a fixed address, set `ARCHIPELAGO_URL`
instead.

## Configuration

Configuration comes from environment variables. A `.env` file in the
working directory is loaded too, if one exists.

| Variable | Default | Notes |
| --- | --- | --- |
| `SEABIRD_HOST` | required | seabird-core URL |
| `SEABIRD_TOKEN` | required | |
| `SEABIRD_ID` | `archipelago` | Backend ID; unique per instance |
| `ARCHIPELAGO_ROOM` | | Room ID or `https://archipelago.gg/room/<id>` |
| `ARCHIPELAGO_URL` | | Fixed server address, `wss://host:port` or `host:port`; wins over `ARCHIPELAGO_ROOM` |
| `ARCHIPELAGO_SLOT` | required | Slot name to connect as |
| `ARCHIPELAGO_PASSWORD` | | Room password |
| `ARCHIPELAGO_CHANNEL` | `room` | Seabird channel ID |
| `ARCHIPELAGO_COMMAND_PREFIX` | `.` | Can't start with `!` |
| `ARCHIPELAGO_ITEM_FILTER` | all classes | Comma list of `progression`, `progression_skip_balancing`, `useful`, `filler`, `trap`; applies to sends and hints |
| `ARCHIPELAGO_DEATHLINK` | `true` | Announce deathlinks |
| `ARCHIPELAGO_CHAT` | `true` | Relay player chat and allow seabird to chat in the room |
| `LOG_FORMAT` | `json` | `json`, `pretty`, or `text` |
| `LOG_LEVEL` | `info` | |

One of `ARCHIPELAGO_ROOM` or `ARCHIPELAGO_URL` is required. The channel's
seabird URI is `archipelago://<SEABIRD_ID>/<ARCHIPELAGO_CHANNEL>`, which is
what the proxy plugin uses to relay the room into Discord or IRC.

## Development

`nix develop` provides the toolchain. `just check` runs formatting, lint,
and tests, the same as CI. `just run` starts the backend from source using
`.env`.
