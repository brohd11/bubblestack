package components

func (s *ModularScreen) applySplitDelta(g *layoutBranch, index, delta int) {
	if index < 0 || index+1 >= len(g.children) || delta == 0 {
		return
	}
	if g.children[index].fixed || g.children[index+1].fixed {
		return
	}
	lengths := make([]int, len(g.children))
	mins := make([]int, len(g.children))
	for i, c := range g.children {
		lengths[i], mins[i] = c.rect.w, c.minW
		if g.axis == LayoutVertical {
			lengths[i], mins[i] = c.rect.h, c.minH
		}
	}
	a, b := index, index+1
	lo, hi := mins[a]-lengths[a], lengths[b]-mins[b]
	if lo > hi {
		return
	} // the terminal cannot currently fit both minima
	delta = max(lo, min(delta, hi))
	lengths[a] += delta
	lengths[b] -= delta
	// Preserve other siblings' rendered extents. A fixed/weighted boundary must
	// not resize unrelated weighted siblings elsewhere in the same group.
	for i := range g.children {
		if g.sizes[i] > 0 {
			g.sizes[i] = lengths[i]
		} else {
			g.weights[i] = float64(max(1, lengths[i]))
		}
	}
}

func (s *ModularScreen) nudgeLayout(dw, dh int) {
	move := func(axis LayoutAxis, delta int) {
		if delta == 0 {
			return
		}
		for child := s.layoutLeaves[s.focus]; child.parent != nil; child = child.parent {
			g := child.parent
			if g.axis != axis || len(g.children) < 2 {
				continue
			}
			index := min(child.index, len(g.children)-2)
			if g.children[index].fixed || g.children[index+1].fixed {
				continue
			}
			s.applySplitDelta(g, index, delta)
			return
		}
	}
	move(LayoutHorizontal, dw)
	move(LayoutVertical, dh)
	s.relayout()
}
