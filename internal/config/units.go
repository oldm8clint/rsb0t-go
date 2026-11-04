package config

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Duration is a time.Duration written as a Go duration string such as "16s" or "5m".
// Why: a bare number leaves the unit to guesswork, so 0 is the only number accepted.
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return formatDuration(time.Duration(d)) }

// MarshalJSON writes the duration string form.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON reads a duration string, or the number 0.
func (d *Duration) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "0" {
		*d = 0
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("want a duration string such as \"16s\" or \"5m\", got %s", jsonKind(b))
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("want a duration such as \"16s\" or \"5m\", got %q", s)
	}
	*d = Duration(v)
	return nil
}

// formatDuration drops the zero tails time.Duration prints, so 5m reads "5m" not "5m0s".
func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// ByteSize is a byte count written as a number of bytes or a string with a binary unit.
type ByteSize int64

// Binary size units.
const (
	KiB ByteSize = 1 << 10
	MiB ByteSize = 1 << 20
	GiB ByteSize = 1 << 30
)

// ParseByteSize reads "123", "512KiB", "50MiB" or "2GiB". Decimal units are refused because
// "50MB" beside a GOMEMLIMIT-style "50MiB" is an easy 5% mistake.
func ParseByteSize(s string) (ByteSize, error) {
	t := strings.TrimSpace(s)
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("want a size such as \"50MiB\", got %q", s)
	}
	n, err := strconv.ParseInt(t[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	var unit ByteSize
	switch strings.ToLower(strings.TrimSpace(t[i:])) {
	case "", "b":
		unit = 1
	case "kib":
		unit = KiB
	case "mib":
		unit = MiB
	case "gib":
		unit = GiB
	case "kb", "mb", "gb":
		return 0, fmt.Errorf("size %q: use the binary units KiB, MiB or GiB", s)
	default:
		return 0, fmt.Errorf("want a size such as \"50MiB\", got %q", s)
	}
	if n > math.MaxInt64/int64(unit) {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	return ByteSize(n) * unit, nil
}

func (b ByteSize) String() string {
	switch {
	case b != 0 && b%GiB == 0:
		return strconv.FormatInt(int64(b/GiB), 10) + "GiB"
	case b != 0 && b%MiB == 0:
		return strconv.FormatInt(int64(b/MiB), 10) + "MiB"
	case b != 0 && b%KiB == 0:
		return strconv.FormatInt(int64(b/KiB), 10) + "KiB"
	}
	return strconv.FormatInt(int64(b), 10) + "B"
}

// MarshalJSON writes the string form.
func (b ByteSize) MarshalJSON() ([]byte, error) { return json.Marshal(b.String()) }

// UnmarshalJSON reads a byte count or a size string.
func (b *ByteSize) UnmarshalJSON(data []byte) error {
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		if n < 0 {
			return fmt.Errorf("want a size of at least 0, got %d", n)
		}
		*b = ByteSize(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("want a size such as \"50MiB\", got %s", jsonKind(data))
	}
	v, err := ParseByteSize(s)
	if err != nil {
		return err
	}
	*b = v
	return nil
}

// Secret holds a password. It prints and marshals as [redacted] so a logged Account cannot leak it.
type Secret string

const redacted = "[redacted]"

func (Secret) String() string { return redacted }

// GoString keeps %#v from printing the value.
func (Secret) GoString() string { return redacted }

// MarshalJSON writes [redacted].
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

// Reveal returns the password, for the login block and nothing else.
func (s Secret) Reveal() string { return string(s) }
