// Package handoff defines the narrow boundary between account provisioning and
// the fleet controller. It intentionally contains no transport or orchestration
// implementation so either side can evolve independently.
package handoff

import "context"

type Account struct {
	ID            string
	Provider      string
	SecretRef     string
	ScriptProfile string
	RequestedNode string
}

type Request struct{ Account Account }

type Assignment struct {
	AccountID string
	NodeID    string
	Script    string
}

// Controller accepts accounts that have already completed provider-supported
// authentication and local readiness checks. Authentication material remains
// outside this interface.
type Controller interface {
	AssignReadyAccount(context.Context, Request) (Assignment, error)
}
