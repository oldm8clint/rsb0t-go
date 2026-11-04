package fleetrunner

// AttackerNames is the name of every NPC now facing this account, one per NPC, for the pen
// avoidance a break-off facing something other than the herd starts (scarabplan.ForeignAttacker).
func AttackerNames(d *Deps) []string {
	st := d.Session.State()
	self := st.Self()
	if self < 0 {
		return nil
	}
	var out []string
	for _, n := range st.NPCs() {
		if !n.TargetsPlayer(self) {
			continue
		}
		name := "npc"
		if d.Config != nil {
			if info, ok := d.Config.Npc(n.Type); ok && info.Name != "" {
				name = info.Name
			}
		}
		out = append(out, name)
	}
	return out
}
