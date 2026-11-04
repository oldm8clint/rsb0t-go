package fleetplan

func runePens() []TrainPen {
	var out []TrainPen
	for _, p := range TrainPens {
		if p.RunePen {
			out = append(out, p)
		}
	}
	return out
}
