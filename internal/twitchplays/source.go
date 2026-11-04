package twitchplays

import "context"

// ChatSource is the platform-neutral ingestion boundary. Implementations
// should only translate external events; parsing, policy and game control stay
// inside this package.
type ChatSource interface {
	Next(context.Context) (ChatEvent, error)
}
