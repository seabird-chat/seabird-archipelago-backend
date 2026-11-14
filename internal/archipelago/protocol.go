// Package archipelago implements the client side of the Archipelago
// multiworld network protocol, limited to what a text-only client needs.
//
// Protocol reference:
// https://github.com/ArchipelagoMW/Archipelago/blob/main/docs/network%20protocol.md
package archipelago

import "encoding/json"

// Item flags from NetworkItem.flags. SkipBalancing and Deprioritized only
// qualify progression items.
const (
	FlagProgression   = 0b00001
	FlagUseful        = 0b00010
	FlagTrap          = 0b00100
	FlagSkipBalancing = 0b01000
	FlagDeprioritized = 0b10000
)

// Client tags sent in Connect.
const (
	TagTextOnly  = "TextOnly"
	TagDeathLink = "DeathLink"
)

// PrintJSON types this package cares about. Unknown types are still valid.
const (
	PrintItemSend           = "ItemSend"
	PrintItemCheat          = "ItemCheat"
	PrintHint               = "Hint"
	PrintJoin               = "Join"
	PrintPart               = "Part"
	PrintChat               = "Chat"
	PrintServerChat         = "ServerChat"
	PrintTutorial           = "Tutorial"
	PrintTagsChanged        = "TagsChanged"
	PrintCommandResult      = "CommandResult"
	PrintAdminCommandResult = "AdminCommandResult"
	PrintGoal               = "Goal"
	PrintRelease            = "Release"
	PrintCollect            = "Collect"
	PrintCountdown          = "Countdown"
)

// JSONMessagePart types.
const (
	PartText         = "text"
	PartPlayerID     = "player_id"
	PartPlayerName   = "player_name"
	PartItemID       = "item_id"
	PartItemName     = "item_name"
	PartLocationID   = "location_id"
	PartLocationName = "location_name"
	PartEntranceName = "entrance_name"
	PartHintStatus   = "hint_status"
	PartColor        = "color"
)

// Packet is a single command from a server frame, with its raw body kept so
// it can be decoded into the matching type.
type Packet struct {
	Cmd string
	Raw json.RawMessage
}

// Decode unmarshals the packet body into v.
func (p Packet) Decode(v any) error {
	return json.Unmarshal(p.Raw, v)
}

type NetworkVersion struct {
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Build int    `json:"build"`
	Class string `json:"class"`
}

type NetworkPlayer struct {
	Team  int    `json:"team"`
	Slot  int    `json:"slot"`
	Alias string `json:"alias"`
	Name  string `json:"name"`
}

type NetworkSlot struct {
	Name         string `json:"name"`
	Game         string `json:"game"`
	Type         int    `json:"type"`
	GroupMembers []int  `json:"group_members"`
}

type NetworkItem struct {
	Item     int64 `json:"item"`
	Location int64 `json:"location"`
	Player   int   `json:"player"`
	Flags    int   `json:"flags"`
}

type JSONMessagePart struct {
	Type   string `json:"type,omitempty"`
	Text   string `json:"text,omitempty"`
	Color  string `json:"color,omitempty"`
	Flags  int    `json:"flags,omitempty"`
	Player int    `json:"player,omitempty"`
}

type Hint struct {
	ReceivingPlayer int    `json:"receiving_player"`
	FindingPlayer   int    `json:"finding_player"`
	Location        int64  `json:"location"`
	Item            int64  `json:"item"`
	Found           bool   `json:"found"`
	Entrance        string `json:"entrance"`
	ItemFlags       int    `json:"item_flags"`
	Status          int    `json:"status"`
}

// Server to client packets.

type RoomInfo struct {
	Version              NetworkVersion    `json:"version"`
	Tags                 []string          `json:"tags"`
	Password             bool              `json:"password"`
	Games                []string          `json:"games"`
	DataPackageChecksums map[string]string `json:"datapackage_checksums"`
	SeedName             string            `json:"seed_name"`
}

type ConnectionRefused struct {
	Errors []string `json:"errors"`
}

type Connected struct {
	Team     int                 `json:"team"`
	Slot     int                 `json:"slot"`
	Players  []NetworkPlayer     `json:"players"`
	SlotInfo map[int]NetworkSlot `json:"slot_info"`
}

type RoomUpdate struct {
	Players []NetworkPlayer `json:"players"`
}

type PrintJSON struct {
	Data      []JSONMessagePart `json:"data"`
	Type      string            `json:"type"`
	Receiving int               `json:"receiving"`
	Item      NetworkItem       `json:"item"`
	Found     bool              `json:"found"`
	Team      int               `json:"team"`
	Slot      int               `json:"slot"`
	Message   string            `json:"message"`
	Tags      []string          `json:"tags"`
	Countdown int               `json:"countdown"`
}

type GameData struct {
	ItemNameToID     map[string]int64 `json:"item_name_to_id"`
	LocationNameToID map[string]int64 `json:"location_name_to_id"`
	Checksum         string           `json:"checksum"`
}

type DataPackage struct {
	Data struct {
		Games map[string]GameData `json:"games"`
	} `json:"data"`
}

type Bounced struct {
	Games []string        `json:"games"`
	Slots []int           `json:"slots"`
	Tags  []string        `json:"tags"`
	Data  json.RawMessage `json:"data"`
}

// DeathLink is the data carried by a Bounced packet tagged DeathLink.
type DeathLink struct {
	Time   float64 `json:"time"`
	Cause  string  `json:"cause"`
	Source string  `json:"source"`
}

type Retrieved struct {
	Keys map[string]json.RawMessage `json:"keys"`
}

type SetReply struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// Client to server packets. Each carries its cmd so a slice of them can be
// marshaled directly as a frame.

type connectPacket struct {
	Cmd           string         `json:"cmd"`
	Password      string         `json:"password"`
	Game          string         `json:"game"`
	Name          string         `json:"name"`
	UUID          string         `json:"uuid"`
	Version       NetworkVersion `json:"version"`
	ItemsHandling int            `json:"items_handling"`
	Tags          []string       `json:"tags"`
	SlotData      bool           `json:"slot_data"`
}

type getDataPackagePacket struct {
	Cmd   string   `json:"cmd"`
	Games []string `json:"games"`
}

type sayPacket struct {
	Cmd  string `json:"cmd"`
	Text string `json:"text"`
}

type keysPacket struct {
	Cmd  string   `json:"cmd"`
	Keys []string `json:"keys"`
}
