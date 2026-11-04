package twitchplays

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Command is the normalized representation of a crowd instruction. Chat text
// never reaches the game client directly; it must first become a bounded command.
type Command struct {
	Action    string
	Args      []string
	Source    string
	Author    string
	MessageID string
	Received  time.Time
}

// AllowedActions is intentionally small. New actions should be added only when
// the corresponding game-side operation has tests and a rate-limit policy.
//
// There is deliberately no bank action: a crowd must never be able to move an
// account's stored items. See ForbiddenActions.
var AllowedActions = map[string]struct{}{
	"walk": {}, "fish": {}, "interact": {}, "wait": {}, "stop": {},
}

// ForbiddenActions are commands a crowd may type that are never executed, with the
// reason a viewer is told. Anything that could move items off or out of the account
// (banking, trading, dropping, giving away, selling) is refused here, before any
// game-side code sees it, whatever the arguments. A forbidden command is also never
// a valid Command, so no queue, controller or sink can be handed one.
var ForbiddenActions = map[string]string{
	"bank":     "banking is not allowed in CrowdPlay",
	"deposit":  "banking is not allowed in CrowdPlay",
	"withdraw": "banking is not allowed in CrowdPlay",
	"trade":    "trading is not allowed in CrowdPlay",
	"drop":     "dropping items is not allowed in CrowdPlay",
	"give":     "giving items away is not allowed in CrowdPlay",
	"giveaway": "giving items away is not allowed in CrowdPlay",
	"sell":     "selling is not allowed in CrowdPlay",
	"buy":      "shops are not allowed in CrowdPlay",
	"stake":    "duels and stakes are not allowed in CrowdPlay",
	"duel":     "duels and stakes are not allowed in CrowdPlay",
	"alch":     "turning items into coins is not allowed in CrowdPlay",
}

// Directions are the compass words !walk takes, as tile steps (x east, z north).
var Directions = map[string][2]int{
	"n": {0, 1}, "s": {0, -1}, "e": {1, 0}, "w": {-1, 0},
	"ne": {1, 1}, "nw": {-1, 1}, "se": {1, -1}, "sw": {-1, -1},
	"north": {0, 1}, "south": {0, -1}, "east": {1, 0}, "west": {-1, 0},
}

// MaxWalkSteps bounds one !walk; DefaultWalkSteps is a !walk with no count.
const (
	MaxWalkSteps     = 10
	DefaultWalkSteps = 3
	maxNameLength    = 24
)

// DeniedTargets are names !interact never acts on, whatever option they offer: the
// bank's own npcs and objects and anyone who trades. A sink applies them again, and
// also refuses by the option it would actually use.
var DeniedTargets = []string{"bank", "booth", "deposit", "chest", "trader", "shop", "store", "exchange", "merchant"}

func Normalize(action string) string { return strings.ToLower(strings.TrimSpace(action)) }

// Refusal names why a chat line is refused when it asks for a forbidden action
// ("!bank", "!trade bob", "!drop all"); "" when it is not one.
func Refusal(text string) string {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "!") {
		return ""
	}
	return ForbiddenActions[Normalize(strings.TrimPrefix(parts[0], "!"))]
}

// Walk returns the step and tile count of a valid !walk.
func Walk(c Command) (dx, dz, steps int, err error) {
	if len(c.Args) < 1 || len(c.Args) > 2 {
		return 0, 0, 0, fmt.Errorf("say !walk <n|s|e|w|ne|nw|se|sw> [1-%d]", MaxWalkSteps)
	}
	d, ok := Directions[Normalize(c.Args[0])]
	if !ok {
		return 0, 0, 0, fmt.Errorf("unknown direction %q", c.Args[0])
	}
	steps = DefaultWalkSteps
	if len(c.Args) == 2 {
		n, err := strconv.Atoi(c.Args[1])
		if err != nil || n < 1 || n > MaxWalkSteps {
			return 0, 0, 0, fmt.Errorf("walk 1-%d tiles", MaxWalkSteps)
		}
		steps = n
	}
	return d[0], d[1], steps, nil
}

// InteractTarget returns the name a valid !interact names (lower case).
func InteractTarget(c Command) (string, error) {
	name := Normalize(strings.Join(c.Args, " "))
	if name == "" || len(name) > maxNameLength || len(c.Args) > 3 {
		return "", fmt.Errorf("say !interact <name>")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r == ' ' || r == '-' || r == '\'') {
			return "", fmt.Errorf("names are letters only")
		}
	}
	if DeniedTarget(name) {
		return "", fmt.Errorf("%q is not allowed in CrowdPlay", name)
	}
	return name, nil
}

// DeniedTarget reports whether a name contains one of DeniedTargets.
func DeniedTarget(name string) bool {
	name = strings.ToLower(name)
	for _, d := range DeniedTargets {
		if strings.Contains(name, d) {
			return true
		}
	}
	return false
}

// ValidateArgs checks a command's arguments against its action's own grammar.
func ValidateArgs(c Command) error {
	switch Normalize(c.Action) {
	case "walk":
		_, _, _, err := Walk(c)
		return err
	case "interact":
		_, err := InteractTarget(c)
		return err
	case "fish", "wait", "stop":
		if len(c.Args) > 2 {
			return fmt.Errorf("!%s takes no arguments", Normalize(c.Action))
		}
		return nil
	}
	return fmt.Errorf("unknown command")
}

// Validate reports whether c may be queued: an allowed (never a forbidden) action
// with well-formed arguments, a message id and an author.
func Validate(c Command) bool {
	a := Normalize(c.Action)
	if _, forbidden := ForbiddenActions[a]; forbidden {
		return false
	}
	_, ok := AllowedActions[a]
	return ok && c.MessageID != "" && c.Author != "" && ValidateArgs(c) == nil
}
