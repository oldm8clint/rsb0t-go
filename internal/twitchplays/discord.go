package twitchplays

import "time"

// DiscordMessage is the small subset of a Discord message needed by
// CrowdPlay. Keeping this DTO dependency-free lets the offline lab compile
// without a Discord SDK or credentials.
type DiscordMessage struct {
	ID         string
	AuthorID   string
	AuthorName string
	Content    string
	ChannelID  string
	Received   time.Time
}

// DiscordEvent converts a Discord chat message into the common ChatEvent.
// A real gateway adapter can populate this DTO from its Discord SDK.
func DiscordEvent(m DiscordMessage) ChatEvent {
	author := m.AuthorID
	if author == "" {
		author = m.AuthorName
	}
	return ChatEvent{MessageID: m.ID, Author: author, Text: m.Content, Source: "discord", Received: m.Received}
}
