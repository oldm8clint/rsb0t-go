package client

// ResponseMeaning is what a login response code means, in plain English, for the message an operator
// reads on the panel. "" for a code with nothing useful to add. The codes are the vendored 289 client's
// own (Client.ts's loginMes1/loginMes2) and the engine's (World.ts).
func ResponseMeaning(code int) string {
	switch code {
	case 3:
		return "the server says the login name or password is wrong"
	case 4:
		return "the server says this account is disabled"
	case AlreadyLoggedInResponse:
		return "this account is already logged in somewhere else; log it out there first"
	case 6:
		return "the server says this client is out of date: it most likely runs a different game revision than 289, " +
			"or the login key (modulus/exponent) or the cache checksums do not match it (a server that changes its key when it restarts answers this " +
			"until the new key is read); if a key was pasted, paste the server's current one"
	case WorldFullResponse:
		return "this world is full; add another world to the roster, or try again later"
	case 8:
		return "the server's login service is offline; try again later"
	case 9:
		return "too many connections from this address"
	case 11:
		return "the login server rejected the session"
	case 12:
		return "this is a members-only world and the account is not a member"
	case LoginAttemptsExceededResponse:
		return "too many login attempts from this address; logins slow down and try again"
	}
	return ""
}
