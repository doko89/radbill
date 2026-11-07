package radius

import "testing"

func TestParseNotify(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    NotifyEvent
		wantErr bool
	}{
		{name: "pppoe upsert", payload: `{"type":"pppoe","action":"upsert","id":"a1"}`, want: NotifyEvent{Type: "pppoe", Action: "upsert", ID: "a1"}},
		{name: "nas delete", payload: `{"type":"nas","action":"delete","id":"n9"}`, want: NotifyEvent{Type: "nas", Action: "delete", ID: "n9"}},
		{name: "package upsert", payload: `{"type":"package","action":"upsert","id":"p3"}`, want: NotifyEvent{Type: "package", Action: "upsert", ID: "p3"}},
		{name: "package delete", payload: `{"type":"package","action":"delete","id":"p3"}`, want: NotifyEvent{Type: "package", Action: "delete", ID: "p3"}},
		{name: "not json", payload: `nope`, wantErr: true},
		{name: "empty", payload: ``, wantErr: true},
		{name: "unknown type", payload: `{"type":"invoice","action":"upsert","id":"i1"}`, wantErr: true},
		{name: "unknown action", payload: `{"type":"nas","action":"purge","id":"n1"}`, wantErr: true},
		{name: "missing id", payload: `{"type":"nas","action":"upsert"}`, wantErr: true},
		{name: "empty id", payload: `{"type":"nas","action":"upsert","id":""}`, wantErr: true},
		{name: "missing type", payload: `{"action":"upsert","id":"x"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseNotify([]byte(tt.payload))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseNotify(%q) = %+v, want error", tt.payload, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseNotify(%q) error: %v", tt.payload, err)
			}
			if got != tt.want {
				t.Fatalf("ParseNotify(%q) = %+v, want %+v", tt.payload, got, tt.want)
			}
		})
	}
}

func TestParseNotifyIgnoresExtraFields(t *testing.T) {
	ev, err := ParseNotify([]byte(`{"type":"nas","action":"upsert","id":"n1","extra":1}`))
	if err != nil || ev.ID != "n1" || ev.Type != "nas" || ev.Action != "upsert" {
		t.Fatalf("got %+v, %v", ev, err)
	}
}
