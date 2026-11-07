package domain

import (
	"strings"
	"testing"
)

func TestRenderPlaceholders(t *testing.T) {
	got, err := Render("Halo {{.Nama}}, tagihan {{.Nomor}} Rp{{.Total}}.", map[string]string{
		"Nama": "Budi", "Nomor": "INV-1", "Total": "50000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Halo Budi, tagihan INV-1 Rp50000." {
		t.Fatalf("render = %q", got)
	}
}

func TestRenderUnknownPlaceholderIsEmpty(t *testing.T) {
	got, err := Render("Hi {{.Nama}} {{.Typo}}!", map[string]string{"Nama": "Ani"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Typo") || strings.Contains(got, "no value") {
		t.Fatalf("typo leaked into output: %q", got)
	}
	if got != "Hi Ani !" {
		t.Fatalf("render = %q", got)
	}
}

func TestRenderSyntaxError(t *testing.T) {
	if _, err := Render("Hi {{.Nama", map[string]string{}); err == nil {
		t.Fatal("bad syntax accepted")
	}
}

func TestSubjectFor(t *testing.T) {
	if got := SubjectFor(KindPaid, map[string]string{"Nomor": "INV-9"}); !strings.Contains(got, "INV-9") {
		t.Fatalf("subject = %q", got)
	}
}

func TestDefaultTemplatesRender(t *testing.T) {
	data := map[string]string{"Nama": "N", "Nomor": "I", "Total": "1", "JatuhTempo": "D", "Username": "U", "Perusahaan": "P"}
	for _, kind := range []string{KindInvoiceIssued, KindReminder, KindIsolated, KindPaid} {
		out, err := Render(DefaultTemplate(kind), data)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if strings.Contains(out, "{{") {
			t.Fatalf("%s left placeholders: %q", kind, out)
		}
	}
}
