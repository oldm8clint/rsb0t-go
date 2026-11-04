package actions

// LogoutButton is the logout tab's real "Click here to logout" button component id.
//
// This one isn't a literal already sitting in rsb0t-main (unlike Trade's, which came
// straight from src/bot/api/trade/Trade.ts) — nobody had ported client-side logout
// before, so it was found the way internal/archive exists to find ids that aren't
// hardcoded anywhere: parsed the live engine's interface archive (cmd/archivetest's
// fetch-and-parse sequence) and matched the label text against the interface's own
// source, content/scripts/player/interfaces/logout.if:
//
//	[try_logout]
//	type=text
//	buttontype=normal
//	clientcode=205
//	text=Click here to logout
//
// That search found exactly one match: component id 2458 (layer/root interface 2449),
// TYPE_TEXT with BUTTON_OK (buttontype=normal compiles to Component.BUTTON=1, see
// engine/src/cache/config/Component.ts:17), Text=="Click here to logout" — same label,
// same clientcode as the .if source. Pinned once here the same way Bank's 5292 main
// modal id is pinned in bank.go, rather than re-parsing the archive on every logout.
//
// Why a plain IfButton click is the whole story, despite clientcode=205: Client.ts's
// IF_BUTTON dispatch (doAction, ~9346-9358) only skips sending IF_BUTTON to the server
// when a component has a clientCode AND clientButton(com) returns false for it. For
// CC_LOGOUT specifically (ClientCode.ts:14, value 205), clientButton (~11159-11161)
// sets a local logoutTimer (a UI countdown / idle-disconnect bookkeeping value — see
// clientButton's other branches and the idle-timer use of the same field around
// Client.ts:2513) and returns true, so notify stays true and the client sends the
// exact same IF_BUTTON + p2(comID) bytes a plain server-side button would. The
// interface's own server script confirms the server does the real work from that
// click alone (content/scripts/login_logout/logout.rs2's
// `[if_button,logout:try_logout]`: checks duel-arena/gnomeball-pitch/combat lockouts,
// then `p_logout`) — ServerProt.LOGOUT (Client.ts ~6965-6970, handled by the private
// logout() at ~2603-2626) is that script's reply telling the client its socket can
// close, not something the client sends first.
const LogoutButton = 2458

// Logout ports the logout tab's button click: a plain IfButton on LogoutButton. The
// server may refuse it (combat lockout, duel arena, a gnomeball pitch — see
// content/scripts/login_logout/logout.rs2) by just not sending ServerProt.LOGOUT back;
// this function only sends the click; watching for the server's reply is the caller's
// job (see gamestate.State.IsLoggedOut, driven by client.Session.Logout).
func Logout(s Session) error {
	return IfButton(s, LogoutButton)
}
