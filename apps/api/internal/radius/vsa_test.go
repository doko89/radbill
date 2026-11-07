package radius

import (
	"bytes"
	"strings"
	"testing"

	lradius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

func findVSA(t *testing.T, p *lradius.Packet) lradius.Attribute {
	t.Helper()
	for _, avp := range p.Attributes {
		if avp.Type == rfc2865.VendorSpecific_Type {
			return avp.Attribute
		}
	}
	t.Fatal("no Vendor-Specific attribute found")
	return nil
}

// TestMikrotikAddressListBytes pins the exact wire encoding of one VSA:
// Vendor-Specific(26) = Vendor-Id 14988 (00 00 3A 8C) + type 19, len 8,
// "isolir". Attribute numbers come from dictionary.mikrotik, not guesses.
func TestMikrotikAddressListBytes(t *testing.T) {
	if VendorIDMikrotik != 14988 || MikrotikAttrAddressList != 19 {
		t.Fatalf("constants drifted: vendor=%d attr=%d", VendorIDMikrotik, MikrotikAttrAddressList)
	}
	p := lradius.New(lradius.CodeAccessAccept, []byte("secret"))
	if err := AddMikrotikString(p, MikrotikAttrAddressList, "isolir"); err != nil {
		t.Fatal(err)
	}
	got := findVSA(t, p)
	want := []byte{0x00, 0x00, 0x3a, 0x8c, 19, 8, 'i', 's', 'o', 'l', 'i', 'r'}
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("VSA bytes = % x, want % x", []byte(got), want)
	}
}

func TestMikrotikUint32Bytes(t *testing.T) {
	if MikrotikAttrTotalLimit != 17 || MikrotikAttrTotalLimitGigawords != 18 || MikrotikAttrRateLimit != 8 {
		t.Fatal("mikrotik attribute constants drifted")
	}
	p := lradius.New(lradius.CodeAccessAccept, []byte("secret"))
	if err := AddMikrotikUint32(p, MikrotikAttrTotalLimit, 0x12345678); err != nil {
		t.Fatal(err)
	}
	got := findVSA(t, p)
	want := []byte{0x00, 0x00, 0x3a, 0x8c, 17, 6, 0x12, 0x34, 0x56, 0x78}
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("VSA bytes = % x, want % x", []byte(got), want)
	}
}

func TestMikrotikStringTooLong(t *testing.T) {
	p := lradius.New(lradius.CodeAccessAccept, []byte("secret"))
	if err := AddMikrotikString(p, MikrotikAttrRateLimit, strings.Repeat("x", 248)); err == nil {
		t.Fatal("expected error for oversized VSA string")
	}
}
