package questplan

import "fmt"

// Shield of Arrav is a two-account quest, and Heroes' Quest is another: this is the pure pairing plan both share (content 289:
// quests/quest_blackarmgang, areas/area_varrock curator, king_roald, katrine, straven; quests/quest_hero).
//
// The content, as it matters to a pair:
//   - two varps: %phoenixgang (1 started, 2 read the book, 3 spoke to Reldo, 4 to Baraek, 8 to Straven, 9 joined, complete) and
//     %blackarmgang (1 started, 2 spoke to Katrine, 3 joined, complete). An account is in ONE gang.
//   - each hideout holds one half of the shield, and only a member gets in: the Phoenix chest gives arravshield1 (door: %phoenixgang >=
//     joined), the Black Arm cupboard gives arravshield2 (door: %blackarmgang >= joined).
//   - the Curator takes BOTH halves from one member of either gang and writes TWO certificates. The account that handed in both halves
//     gives the other its certificate, so a pair meets twice: the second account hands over its half, the first hands back a certificate.
//   - King Roald pays a certificate to a member of either gang: 600 coins and the quest (the account's own gang's varp completes).
//   - Heroes' Quest needs ONE of the two complete, and its armband is also a pair (the Phoenix side must be complete and past Charlie to
//     be allowed to kill Grip, so a Black Arm hero needs a Phoenix hero beside it).
//
// So the unit is a pair (one Phoenix, one Black Arm), made once and used for both quests, and the one thing the pair needs from the world
// is a rendezvous: the two accounts in the museum together (Varrock), twice.

// Gang is the side an account joins.
type Gang int

const (
	GangNone Gang = iota
	GangPhoenix
	GangBlackArm
)

func (g Gang) String() string {
	switch g {
	case GangPhoenix:
		return "phoenix"
	case GangBlackArm:
		return "blackarm"
	}
	return "none"
}

// Other is the opposite gang.
func (g Gang) Other() Gang {
	switch g {
	case GangPhoenix:
		return GangBlackArm
	case GangBlackArm:
		return GangPhoenix
	}
	return GangNone
}

// The varp stages the plan reads.
const (
	PhoenixJoined    = 9
	PhoenixComplete  = 10
	BlackArmJoined   = 3
	BlackArmComplete = 4
)

// ArravMe is what one account knows of itself.
type ArravMe struct {
	Gang       Gang
	Stage      int  // the account's own gang's varp
	HasHalf    bool // holds its gang's half shield
	HasBoth    bool // holds both halves
	HasCert    bool // holds a certificate
	CertIsMine bool // the certificate it holds is one it keeps (it handed in both halves, so it holds the pair's second, to give away, when false)
	Certs      int  // certificates held: two means the account handed both halves in and owes the partner one
}

// ArravPartnerView is what the account learned of the partner (the partner's own heartbeat on the fleet board, never a guess).
type ArravPartnerView struct {
	Present  bool // the partner is in the same museum room and was heard within the freshness window
	Gang     Gang
	Stage    int
	HasHalf  bool
	HasBoth  bool // holds both halves (it is the one that hands them in)
	HasCert  bool
	Complete bool
}

// ArravStep is what the account does next.
type ArravStep int

const (
	ArravDoSolo      ArravStep = iota // its own gang's errands: no partner needed yet
	ArravFetchHalf                    // fetch its gang's half shield from its hideout
	ArravWaitPartner                  // go to the museum and wait (bounded) for the partner
	ArravGiveHalf                     // trade the half to the partner, who holds the other
	ArravHandIn                       // holds both halves: give them to the Curator for two certificates
	ArravGiveCert                     // trade the partner its certificate
	ArravTakeCert                     // wait for the partner's certificate
	ArravClaimReward                  // take the certificate to King Roald
	ArravDone                         // complete
	ArravStop                         // a precondition failed: report and stop
)

func (s ArravStep) String() string {
	return [...]string{"solo", "fetch-half", "wait-partner", "give-half", "hand-in", "give-cert", "take-cert", "claim-reward", "done", "stop"}[s]
}

// ArravJoined is whether the stage reads as joined (or beyond) for the gang.
func ArravJoined(g Gang, stage int) bool {
	switch g {
	case GangPhoenix:
		return stage >= PhoenixJoined
	case GangBlackArm:
		return stage >= BlackArmJoined
	}
	return false
}

// ArravComplete is whether the stage reads as complete for the gang.
func ArravComplete(g Gang, stage int) bool {
	switch g {
	case GangPhoenix:
		return stage >= PhoenixComplete
	case GangBlackArm:
		return stage >= BlackArmComplete
	}
	return false
}

// ArravNext is the account's next step and why. waited is how long it has waited for the partner at the museum and maxWait the bound: past it
// the step is Stop (never an endless wait). A gang membership that is not the opposite of the partner's is a Stop: two of one gang never
// complete the pair.
func ArravNext(me ArravMe, p ArravPartnerView, waited, maxWait int) (ArravStep, string) {
	if me.Gang == GangNone {
		return ArravStop, "no gang chosen: the pair assigns one gang to each account before it starts"
	}
	if ArravComplete(me.Gang, me.Stage) {
		return ArravDone, "the quest is complete"
	}
	if p.Gang != GangNone && p.Gang == me.Gang {
		return ArravStop, fmt.Sprintf("the partner is of the same gang (%s): the halves are in two hideouts, so a pair needs one of each", me.Gang)
	}
	if !ArravJoined(me.Gang, me.Stage) {
		return ArravDoSolo, "the account is not yet in its gang"
	}
	if me.Certs >= 2 {
		if p.Present {
			return ArravGiveCert, "two certificates are held: the partner is here and takes the second"
		}
		return ArravWaitPartner, "two certificates are held: the second is the partner's, wait for it at the museum"
	}
	if me.HasCert {
		return ArravClaimReward, "a certificate is in the pack: King Roald pays it"
	}
	if !me.HasHalf && !me.HasBoth && (p.HasBoth || p.HasCert) {
		return ArravTakeCert, "the half went to the partner: wait for the certificate it hands back"
	}
	if me.HasBoth {
		if p.Present {
			return ArravHandIn, "both halves are held and the partner is here: the Curator writes two certificates"
		}
		return ArravWaitPartner, "both halves are held: wait for the partner at the museum, who takes the second certificate"
	}
	if !me.HasHalf {
		return ArravFetchHalf, "its gang's half shield is not in the pack"
	}
	// holds exactly its own half
	if !p.Present {
		if waited >= maxWait {
			return ArravStop, fmt.Sprintf("the partner did not reach the museum in %d (stop rule)", maxWait)
		}
		return ArravWaitPartner, "the half is in hand: meet the partner at the museum"
	}
	// the one with the lower-ranked gang gives its half, the other hands both in, so the roles never deadlock
	if me.Gang == GangBlackArm {
		return ArravGiveHalf, "the Black Arm account gives its half to the Phoenix account, who hands both in"
	}
	return ArravTakeCert, "wait for the half from the Black Arm account, then hand both in"
}

// ArravPhoenixHandIn is the Phoenix side's step once it has been given the second half: the shared rule that holds the roles fixed.
func ArravPhoenixHandIn(me ArravMe) (ArravStep, string) {
	if me.Gang != GangPhoenix {
		return ArravStop, "only the Phoenix account hands the shield in"
	}
	if me.HasBoth {
		return ArravHandIn, "both halves are held"
	}
	return ArravTakeCert, "the second half has not come"
}

// Pair is two accounts, one of each gang, and the rule that makes them.
type Pair struct {
	Phoenix, BlackArm string
}

// MakePairs pairs candidate names in the order given, alternating Phoenix and Black Arm: the first of each two is the Phoenix account. An
// odd one out is returned unpaired rather than doubled up. Names are never reused, and a name repeated in the input is paired once.
func MakePairs(names []string) (pairs []Pair, unpaired []string) {
	seen := map[string]bool{}
	var uniq []string
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		uniq = append(uniq, n)
	}
	for i := 0; i+1 < len(uniq); i += 2 {
		pairs = append(pairs, Pair{Phoenix: uniq[i], BlackArm: uniq[i+1]})
	}
	if len(uniq)%2 == 1 {
		unpaired = []string{uniq[len(uniq)-1]}
	}
	return pairs, unpaired
}

// PartnerOf is the partner of name in the pairs, with the gang name takes.
func PartnerOf(pairs []Pair, name string) (partner string, g Gang, ok bool) {
	for _, p := range pairs {
		switch name {
		case p.Phoenix:
			return p.BlackArm, GangPhoenix, true
		case p.BlackArm:
			return p.Phoenix, GangBlackArm, true
		}
	}
	return "", GangNone, false
}

// HeroPairReady is the entry rule of the pair's Heroes' Quest: both accounts in the pair have the same prerequisites (the chain's other
// stages complete and the quest points), because the armband step needs both at Brimhaven together and one cannot finish for the other.
func HeroPairReady(a, b map[string]bool, qpA, qpB int) (bool, string) {
	for _, s := range ThroneChain {
		if s.ID != HeroID {
			continue
		}
		for _, n := range s.Needs {
			if !a[n] || !b[n] {
				return false, fmt.Sprintf("%s is not complete on both accounts", n)
			}
		}
		if qpA < s.QP || qpB < s.QP {
			return false, fmt.Sprintf("both accounts need %d quest points (have %d and %d)", s.QP, qpA, qpB)
		}
	}
	return true, "both accounts have every prerequisite"
}

// ArravTell is what one account of a pair posts on the pair board for the other: its gang and stage, what it holds, and whether it stands in the
// museum waiting for the partner right now. It is a report, never an order: the partner decides from it with ArravNext.
type ArravTell struct {
	Gang     Gang
	Stage    int
	HasHalf  bool
	HasBoth  bool
	HasCert  bool
	Certs    int
	HasKey   bool // holds the Phoenix key (the Phoenix account gives it; the Black Arm account needs it)
	NeedsKey bool // a Black Arm account at the Katrine stage with neither key nor crossbows
	Waiting  bool // standing in the museum, waiting for the partner
	World    int
}

// View is the partner's report as ArravNext reads it. present is whether the partner is waiting in the museum on this account's world and
// reported recently (the caller's clock decides what recent is).
func (t ArravTell) View(present bool) ArravPartnerView {
	return ArravPartnerView{Present: present, Gang: t.Gang, Stage: t.Stage, HasHalf: t.HasHalf, HasBoth: t.HasBoth, HasCert: t.HasCert,
		Complete: ArravComplete(t.Gang, t.Stage)}
}
