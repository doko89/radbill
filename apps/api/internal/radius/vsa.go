package radius

import (
	"fmt"

	lradius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// MikroTik vendor-specific attribute numbers.
//
// Source: dictionary.mikrotik shipped with MikroTik RouterOS and vendored in
// layeh.com/radius (vendors/mikrotik/dictionary.mikrotik, "$Id:
// dictionary.mikrotik,v 1.8 2019/12/20 11:02:37 strods Exp $"), consistent
// with the MikroTik wiki "Manual:RADIUS Client" vendor dictionary.
// Vendor-Id 14988 is assigned to MikroTik by IANA.
const (
	VendorIDMikrotik uint32 = 14988

	// MikrotikAttrRateLimit (8, string) caps up/down rate, e.g. "10M/10M".
	MikrotikAttrRateLimit byte = 8
	// MikrotikAttrTotalLimit (17, integer) caps total session bytes (low 32 bits).
	MikrotikAttrTotalLimit byte = 17
	// MikrotikAttrTotalLimitGigawords (18, integer) extends Total-Limit past 4 GiB.
	MikrotikAttrTotalLimitGigawords byte = 18
	// MikrotikAttrAddressList (19, string) puts the client IP in a firewall address list.
	MikrotikAttrAddressList byte = 19
)

// AddMikrotikString appends a string-valued MikroTik VSA to the packet.
func AddMikrotikString(p *lradius.Packet, attr byte, s string) error {
	if len(s) > 247 {
		return fmt.Errorf("mikrotik vsa %d: string too long (%d bytes)", attr, len(s))
	}
	payload := make(lradius.Attribute, 2+len(s))
	payload[0] = attr
	payload[1] = byte(len(payload))
	copy(payload[2:], s)
	vsa, err := lradius.NewVendorSpecific(VendorIDMikrotik, payload)
	if err != nil {
		return fmt.Errorf("mikrotik vsa %d: %w", attr, err)
	}
	p.Add(rfc2865.VendorSpecific_Type, vsa)
	return nil
}

// AddMikrotikUint32 appends an integer-valued MikroTik VSA to the packet.
func AddMikrotikUint32(p *lradius.Packet, attr byte, v uint32) error {
	intAttr := lradius.NewInteger(v)
	payload := make(lradius.Attribute, 2+len(intAttr))
	payload[0] = attr
	payload[1] = byte(len(payload))
	copy(payload[2:], intAttr)
	vsa, err := lradius.NewVendorSpecific(VendorIDMikrotik, payload)
	if err != nil {
		return fmt.Errorf("mikrotik vsa %d: %w", attr, err)
	}
	p.Add(rfc2865.VendorSpecific_Type, vsa)
	return nil
}
