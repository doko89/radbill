package radius

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	lradius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

func acctPacket(t *testing.T, status rfc2866.AcctStatusType, username, session string,
	in, out uint32, secs uint32, cause *rfc2866.AcctTerminateCause) *lradius.Packet {
	t.Helper()
	p := lradius.New(lradius.CodeAccountingRequest, []byte("storesecret"))
	if username != "" {
		if err := rfc2865.UserName_AddString(p, username); err != nil {
			t.Fatal(err)
		}
	}
	if session != "" {
		if err := rfc2866.AcctSessionID_AddString(p, session); err != nil {
			t.Fatal(err)
		}
	}
	if err := rfc2866.AcctStatusType_Add(p, status); err != nil {
		t.Fatal(err)
	}
	if err := rfc2866.AcctInputOctets_Add(p, rfc2866.AcctInputOctets(in)); err != nil {
		t.Fatal(err)
	}
	if err := rfc2866.AcctOutputOctets_Add(p, rfc2866.AcctOutputOctets(out)); err != nil {
		t.Fatal(err)
	}
	if err := rfc2866.AcctSessionTime_Add(p, rfc2866.AcctSessionTime(secs)); err != nil {
		t.Fatal(err)
	}
	if cause != nil {
		if err := rfc2866.AcctTerminateCause_Add(p, *cause); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func serveAcct(srv *AcctServer, pkt *lradius.Packet, ip string) *lradius.Packet {
	cw := &captureWriter{}
	srv.ServeRADIUS(cw, testRequest(pkt, ip))
	return cw.got
}

func TestAccountingSessionLifecycleSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	defer func() {
		_, _ = db.Exec(`DELETE FROM usage_counters WHERE pppoe_account_id = 'acct-active'`)
		_, _ = db.Exec(`DELETE FROM radius_sessions WHERE nas_id = 'nas-store'`)
	}()

	cache := NewCache()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.LoadAll(ctx, db); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	fixedNow := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	log := slog.New(slog.DiscardHandler)
	tracker := NewSessionTracker()
	writer := NewWriter(db, log)
	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	go writer.Run(wctx)
	srv := &AcctServer{Cache: cache, Log: log, Sessions: tracker, Writer: writer,
		Now: func() time.Time { return fixedNow }}

	nasIP := "192.0.2.10"
	cause := rfc2866.AcctTerminateCause_Value_UserRequest
	steps := []*lradius.Packet{
		acctPacket(t, rfc2866.AcctStatusType_Value_Start, "store-user", "sess-acct", 0, 0, 0, nil),
		acctPacket(t, rfc2866.AcctStatusType_Value_InterimUpdate, "store-user", "sess-acct", 1000, 2000, 60, nil),
		acctPacket(t, rfc2866.AcctStatusType_Value_InterimUpdate, "store-user", "sess-acct", 1000, 2000, 60, nil), // dup
		acctPacket(t, rfc2866.AcctStatusType_Value_InterimUpdate, "store-user", "sess-acct", 1500, 2500, 120, nil),
		acctPacket(t, rfc2866.AcctStatusType_Value_Stop, "store-user", "sess-acct", 1600, 2600, 150, &cause),
		acctPacket(t, rfc2866.AcctStatusType_Value_Stop, "store-user", "sess-acct", 1600, 2600, 150, &cause), // retransmit
	}
	for i, pkt := range steps {
		if got := serveAcct(srv, pkt, nasIP); got == nil || got.Code != lradius.CodeAccountingResponse {
			t.Fatalf("step %d: got %+v, want Accounting-Response", i, got)
		}
	}
	wcancel() // drain + final flush
	select {
	case <-writer.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("writer did not finish drain")
	}

	qctx, qcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer qcancel()
	var in, out, secs int64
	err := db.QueryRowContext(qctx, `SELECT bytes_in, bytes_out, online_seconds FROM usage_counters
		WHERE pppoe_account_id = $1 AND period_start = $2`,
		"acct-active", "2026-11-01T00:00:00Z").Scan(&in, &out, &secs)
	if err != nil {
		t.Fatalf("usage row missing: %v", err)
	}
	if in != 1600 || out != 2600 || secs != 150 {
		t.Fatalf("usage = %d,%d,%d, want 1600,2600,150", in, out, secs)
	}
	var stopped, termcause sql.NullString
	var sin, sout int64
	err = db.QueryRowContext(qctx, `SELECT stopped_at, terminate_cause, bytes_in, bytes_out FROM radius_sessions
		WHERE session_id = $1 AND nas_id = $2`, "sess-acct", "nas-store").Scan(&stopped, &termcause, &sin, &sout)
	if err != nil {
		t.Fatalf("session row missing: %v", err)
	}
	if !stopped.Valid || termcause.String != "User-Request" || sin != 1600 || sout != 2600 {
		t.Fatalf("session row = %+v %+v %d %d", stopped, termcause, sin, sout)
	}
}

func TestAccountingOnOffClosesOrphansSQLite(t *testing.T) {
	db := openTestDB(t)
	seedStoreFixtures(t, db)
	defer func() {
		_, _ = db.Exec(`DELETE FROM radius_sessions WHERE nas_id = 'nas-store'`)
	}()

	cache := NewCache()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cache.LoadAll(ctx, db); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	execTestRow(t, db, `INSERT INTO radius_sessions (id, session_id, pppoe_account_id, nas_id,
		started_at, bytes_in, bytes_out, updated_at)
		VALUES ('sess-row-orphan', 'orphan-1', 'acct-active', 'nas-store', $1, 10, 20, $1)`, testStamp)

	log := slog.New(slog.DiscardHandler)
	tracker := NewSessionTracker()
	tracker.Update(SessionKey{NASID: "nas-store", SessionID: "orphan-1"}, Totals{In: 10, Out: 20})
	writer := NewWriter(db, log)
	wctx, wcancel := context.WithCancel(context.Background())
	defer wcancel()
	go writer.Run(wctx)
	srv := &AcctServer{Cache: cache, Log: log, Sessions: tracker, Writer: writer}

	pkt := lradius.New(lradius.CodeAccountingRequest, []byte("storesecret"))
	if err := rfc2866.AcctStatusType_Add(pkt, rfc2866.AcctStatusType_Value_AccountingOn); err != nil {
		t.Fatal(err)
	}
	if got := serveAcct(srv, pkt, "192.0.2.10"); got == nil || got.Code != lradius.CodeAccountingResponse {
		t.Fatalf("got %+v, want Accounting-Response", got)
	}
	if _, existed := tracker.Update(SessionKey{NASID: "nas-store", SessionID: "orphan-1"}, Totals{}); existed {
		t.Fatal("tracker must forget NAS sessions after Accounting-On")
	}
	wcancel()
	select {
	case <-writer.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("writer did not finish drain")
	}

	qctx, qcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer qcancel()
	var stopped, cause sql.NullString
	err := db.QueryRowContext(qctx, `SELECT stopped_at, terminate_cause FROM radius_sessions
		WHERE session_id = $1`, "orphan-1").Scan(&stopped, &cause)
	if err != nil {
		t.Fatalf("orphan row missing: %v", err)
	}
	if !stopped.Valid || cause.String != OrphanTerminateCause {
		t.Fatalf("orphan row = %+v %+v", stopped, cause)
	}
}

func TestAccountingUnknownNASDrops(t *testing.T) {
	srv := &AcctServer{Cache: testCache(), Log: slog.New(slog.DiscardHandler),
		Sessions: NewSessionTracker(), Writer: NewWriter(nil, slog.New(slog.DiscardHandler))}
	pkt := acctPacket(t, rfc2866.AcctStatusType_Value_Start, "alice", "s1", 0, 0, 0, nil)
	if got := serveAcct(srv, pkt, "203.0.113.9"); got != nil {
		t.Fatalf("unknown NAS must get no response, got %+v", got)
	}
}

func TestAccountingUnknownUserAcks(t *testing.T) {
	srv := &AcctServer{Cache: testCache(), Log: slog.New(slog.DiscardHandler),
		Sessions: NewSessionTracker(), Writer: NewWriter(nil, slog.New(slog.DiscardHandler))}
	pkt := acctPacket(t, rfc2866.AcctStatusType_Value_InterimUpdate, "nobody", "s1", 5, 5, 1, nil)
	got := serveAcct(srv, pkt, testNASIP)
	if got == nil || got.Code != lradius.CodeAccountingResponse {
		t.Fatalf("got %+v, want Accounting-Response", got)
	}
	if n := srv.Writer.QueueLen(); n != 0 {
		t.Fatalf("unknown user must enqueue nothing, queue=%d", n)
	}
}
