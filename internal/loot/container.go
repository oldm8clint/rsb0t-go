package loot

// AN UNOPENED CASKET IS NOT WORTH ITS OWN PRICE ON TOP OF ITS CONTENTS (loot maths audit, 7 Oct 2026).
//
// A casket is a catch (the Big net spots and the Mysterious Old Man bring thousands up) and, opened, a
// source of coins, uncut gems and key halves. Both ends were counted at the market: the casket as an
// item when it arrived (Fishing card, Mysterious Old Man's event), and everything it paid when it was
// opened (the Casket card), and opening it consumed the casket without netting it off. Live on 7 Oct:
// 161,000 caskets valued at 1,000 gp each on the fishing and event cards, 213,000 opened for their
// rewards, so the fleet's value held about 160 million gp of caskets AND what came out of them.
//
// The oyster had the same shape and the owner's answer there was to hide the closed one so the pearls
// are the earnings (HiddenItem). A casket is shown, because its count is a rate the owner reads, but it
// is worth nothing as an item: its value is what it pays when it is opened, counted once, there.
//
// Netting the open instead (Source.Consumes) was rejected: 213,000 opens against 161,000 recorded
// arrivals, so the rest (caskets withdrawn from a bank that nothing recorded arriving) would be
// subtracted without ever having been added.

// IsUnvaluedContainer reports whether an obj is a container whose worth is its contents, counted when
// it is opened: it is shown on the card but priced at nothing.
func IsUnvaluedContainer(id int) bool { return id == ObjCasket }
