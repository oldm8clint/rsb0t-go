// Package config loads the fleet settings file.
//
// Two formats are read. The version 1 format is one JSON document with four top-level keys:
//
//	version   always 1
//	global    process-wide knobs: server profiles, data, login, runtime, log, telemetry, controlPanel
//	defaults  account knobs every account inherits
//	accounts  the accounts, each able to override any default
//
// A file with no version key and any of wsURL, httpBase, sharedPassword or loginDelaySeconds is
// read as the fleet.config.json format cmd/fleet uses, so an existing roster keeps working; see
// parseLegacy. That format keeps a plaintext shared password, and loading it adds a warning.
//
// In the version 1 format precedence is built-in default < defaults < account. Objects merge key
// by key at every depth; arrays and scalars replace. settings merges the same way, except that an
// account naming a different script than defaults does not inherit the defaults' settings,
// because those keys belong to the other script.
//
// Load rejects unknown keys (a typo must not fall back to a default silently), null values,
// out-of-range values and duplicate usernames, and reports every problem it finds with the
// section and the dotted field path, for example `accounts[3] "alice": state.viewRadius`.
//
// Passwords never appear in a version 1 file. Each enabled account's password comes from the
// environment variable named by passwordEnv (which may contain {USER}) or from the file at
// login.credentialsFile, which lives outside the repository and holds username:password lines or
// a JSON object. The resolved value is a Secret, which prints and marshals as [redacted].
package config
