package twitchplays

import (
	"strings"
	"time"
)

// Parse converts a chat message into a bounded command. Unknown commands are
// rejected here; platform-specific metadata is supplied by the caller.
func Parse(text, source, author, messageID string, received time.Time) (Command, bool) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "!") {
		return Command{}, false
	}
	action := Normalize(strings.TrimPrefix(parts[0], "!"))
	c := Command{Action: action, Args: append([]string(nil), parts[1:]...), Source: source, Author: author, MessageID: messageID, Received: received}
	return c, Validate(c)
}
