// Package settings persists one script's own settings for one account, between runs.
//
// reference/server-api-docs.md's SettingsBag/SettingsSchema section documents a bot-scoped
// accessor (`bool`/`num`/`str`/`list`/`tile` with a fallback, `raw()` for the whole object) that
// every upstream bot gets as `this.settings`, and flags it as "not yet built as a scripting-facing
// concept" here — while noting the process-level machinery it should sit on top of already exists:
// internal/config validates one versioned settings file for the whole fleet at startup, and its
// Account.Settings (a json.RawMessage decoded with DecodeSettings) is exactly the shape a script's
// settings look like on disk. What internal/config deliberately does not do, because it is a
// startup-time loader for the whole fleet's config, not a per-script runtime store, is: give a
// running script its own file to load, change, and save back — independent of the shared fleet
// file, keyed by account and script the way MuleReceiver's overlay settings page keeps its
// MuleConfig "between runs under the script's id" (internal/scripts/mulereceiverplan/settings.go's
// own doc comment). That gap is what this package fills.
//
// Store owns the on-disk side: one JSON object per account+script pair, read with Load, written
// atomically with Save (temp file plus rename, the same pattern internal/controlpanel's layout
// store uses, so a crash mid-write never corrupts a settings file). Bag owns the in-memory side:
// an untyped JSON object with the SettingsBag-shaped typed accessors (Bool/Num/Str/List) for
// piecemeal reads, plus Decode/Encode to round-trip a caller's own settings struct whole — the
// pure shape a *plan package (mulereceiverplan.MuleConfig, and any sibling package that grows one)
// already models in memory.
//
// This is deliberately small: no schema/validation framework (SettingDef's min/max/group/showIf
// are a UI-widget concern, not modeled here), no migration system, no in-process caching between
// Load and Save calls — a script that wants both reads its settings once at startup and saves
// again when it changes something, the same lifecycle MuleReceiver's overlay already has.
package settings
