package fleetplan

// SplitQuestTiers splits a ladder's ids, in their order, into the head (ids for which proven is
// true: they hold Stage 5) and the tail (everything else: unproven quests and training rungs). With
// on false the whole list is the head. Neither result shares storage with ids.
func SplitQuestTiers(ids []string, proven func(id string) bool, on bool) (head, tail []string) {
	for _, id := range ids {
		if !on || (proven != nil && proven(id)) {
			head = append(head, id)
			continue
		}
		tail = append(tail, id)
	}
	return head, tail
}
