package radius

// Totals is one accounting sample for a session: cumulative octet counters
// (octets plus gigawords already combined into 64 bits) and the cumulative
// session time in seconds.
type Totals struct {
	In          uint64
	Out         uint64
	SessionTime uint64
}

// Delta is the increment derived from two consecutive samples.
type Delta struct {
	In          uint64
	Out         uint64
	SessionTime uint64
	// Duplicate is true when the sample is byte-identical to the previous
	// one (NAS retransmit); the caller must not write anything.
	Duplicate bool
}

// ComputeDelta derives the increment between the previous and current
// sample of one session. Pure: no I/O.
//
//   - identical samples are duplicates (retransmits) and yield no delta;
//   - growing counters yield the difference;
//   - a shrinking counter means the NAS restarted its counters (session
//     counter reset, or a 32-bit wrap the NAS did not report via
//     Gigawords): the whole current total counts as new usage.
func ComputeDelta(last, cur Totals) Delta {
	if cur == last {
		return Delta{Duplicate: true}
	}
	return Delta{
		In:          growOrReset(last.In, cur.In),
		Out:         growOrReset(last.Out, cur.Out),
		SessionTime: growOrReset(last.SessionTime, cur.SessionTime),
	}
}

func growOrReset(last, cur uint64) uint64 {
	if cur >= last {
		return cur - last
	}
	return cur
}
