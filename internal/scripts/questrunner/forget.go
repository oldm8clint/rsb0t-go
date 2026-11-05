package questrunner

// ForgetFailure clears the failure flags (Snapshot.Failed, and Snapshot.Blocked for a kit) of the named steps. The engine clears a step's flag only
// when that same step later succeeds, so a custom step that has put the world back to where another can be tried again (the Tourist Trap's replay of
// the rescue, which catches Ana again) says so with it: what fails afterwards is a new failure, not the old flag read again by the plan. Keys are
// the custom step names, or "provision:"+kit for a kit. Unknown keys are ignored.
func (e *Engine) ForgetFailure(keys ...string) {
	for _, k := range keys {
		delete(e.failed, k)
		delete(e.blocked, k)
	}
}
