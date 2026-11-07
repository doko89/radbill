package radius

import "testing"

func TestComputeDelta(t *testing.T) {
	tests := []struct {
		name string
		last Totals
		cur  Totals
		want Delta
	}{
		{
			name: "growth",
			last: Totals{In: 100, Out: 200, SessionTime: 10},
			cur:  Totals{In: 150, Out: 260, SessionTime: 70},
			want: Delta{In: 50, Out: 60, SessionTime: 60},
		},
		{
			name: "duplicate",
			last: Totals{In: 100, Out: 200, SessionTime: 10},
			cur:  Totals{In: 100, Out: 200, SessionTime: 10},
			want: Delta{Duplicate: true},
		},
		{
			name: "input reset counts current as new",
			last: Totals{In: 900, Out: 200, SessionTime: 10},
			cur:  Totals{In: 50, Out: 250, SessionTime: 70},
			want: Delta{In: 50, Out: 50, SessionTime: 60},
		},
		{
			name: "output reset counts current as new",
			last: Totals{In: 100, Out: 800, SessionTime: 10},
			cur:  Totals{In: 120, Out: 30, SessionTime: 20},
			want: Delta{In: 20, Out: 30, SessionTime: 10},
		},
		{
			name: "full reset",
			last: Totals{In: 400, Out: 500, SessionTime: 600},
			cur:  Totals{In: 1, Out: 2, SessionTime: 3},
			want: Delta{In: 1, Out: 2, SessionTime: 3},
		},
		{
			name: "gigaword growth",
			last: Totals{In: 1<<32 - 10, Out: 0, SessionTime: 0},
			cur:  Totals{In: 1<<32 + 90, Out: 5, SessionTime: 1},
			want: Delta{In: 100, Out: 5, SessionTime: 1},
		},
		{
			name: "32-bit wrap without gigawords counts current",
			last: Totals{In: 0xffffff00, Out: 0, SessionTime: 0},
			cur:  Totals{In: 0x100, Out: 0, SessionTime: 0},
			want: Delta{In: 0x100},
		},
		{
			name: "session time reset",
			last: Totals{In: 10, Out: 10, SessionTime: 999},
			cur:  Totals{In: 20, Out: 20, SessionTime: 5},
			want: Delta{In: 10, Out: 10, SessionTime: 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComputeDelta(tt.last, tt.cur); got != tt.want {
				t.Fatalf("ComputeDelta(%+v, %+v) = %+v, want %+v", tt.last, tt.cur, got, tt.want)
			}
		})
	}
}

func TestSessionTracker(t *testing.T) {
	tr := NewSessionTracker()
	key := SessionKey{NASID: "n1", SessionID: "s1"}

	d, existed := tr.Update(key, Totals{In: 10, Out: 20, SessionTime: 5})
	if existed {
		t.Fatal("first sample must report existed=false")
	}
	if d.Duplicate || d.In != 10 || d.Out != 20 || d.SessionTime != 5 {
		t.Fatalf("first sample delta = %+v, want counts from zero", d)
	}

	d, existed = tr.Update(key, Totals{In: 10, Out: 20, SessionTime: 5})
	if !existed || !d.Duplicate {
		t.Fatalf("repeat sample = %+v,%v, want duplicate", d, existed)
	}

	d, _ = tr.Update(key, Totals{In: 30, Out: 25, SessionTime: 9})
	if d.Duplicate || d.In != 20 || d.Out != 5 || d.SessionTime != 4 {
		t.Fatalf("second sample delta = %+v", d)
	}

	tr.Remove(key)
	if _, existed := tr.Update(key, Totals{In: 1}); existed {
		t.Fatal("sample after Remove must report existed=false")
	}

	tr.Update(SessionKey{NASID: "n1", SessionID: "s2"}, Totals{In: 1})
	tr.Update(SessionKey{NASID: "n2", SessionID: "s3"}, Totals{In: 1})
	if n := tr.RemoveNAS("n1"); n != 2 {
		t.Fatalf("RemoveNAS(n1) = %d, want 2", n)
	}
	if _, existed := tr.Update(SessionKey{NASID: "n2", SessionID: "s3"}, Totals{In: 2}); !existed {
		t.Fatal("RemoveNAS must not touch other NAS sessions")
	}
}

func TestSessionTrackerStopTombstone(t *testing.T) {
	tr := NewSessionTracker()
	key := SessionKey{NASID: "n1", SessionID: "s1"}
	final := Totals{In: 100, Out: 200, SessionTime: 60}

	tr.Update(key, Totals{In: 50, Out: 100, SessionTime: 30})
	tr.Stop(key, final)

	// Retransmitted Stop with identical totals is a duplicate.
	if d, _ := tr.Update(key, final); !d.Duplicate {
		t.Fatalf("retransmitted stop = %+v, want duplicate", d)
	}
	// Reused session id with different totals reopens from zero.
	d, existed := tr.Update(key, Totals{In: 7, Out: 8, SessionTime: 1})
	if existed || d.Duplicate || d.In != 7 || d.Out != 8 || d.SessionTime != 1 {
		t.Fatalf("reused id = %+v,%v, want fresh delta from zero", d, existed)
	}

	// Reopen clears the tombstone for a fresh Start.
	tr.Stop(key, final)
	tr.Reopen(key)
	if d, existed := tr.Update(key, final); existed || d.Duplicate {
		t.Fatalf("after reopen = %+v,%v, want fresh non-duplicate", d, existed)
	}
}
