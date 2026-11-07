package domain

import (
	"testing"
)

func TestHasPermission(t *testing.T) {
	tests := []struct {
		role string
		perm string
		want bool
	}{
		{RoleAdmin, PermCustomerRead, true},
		{RoleAdmin, PermCustomerWrite, true},
		{RoleAdmin, PermPackageWrite, true},
		{RoleAdmin, PermPPPoEWrite, true},
		{RoleAdmin, PermNASWrite, true},
		{RoleAdmin, PermInvoiceWrite, true},
		{RoleAdmin, PermPaymentWrite, true},
		{RoleAdmin, PermSessionRead, true},
		{RoleAdmin, PermSettingsWrite, true},
		{RoleAdmin, PermAuditRead, true},
		{RoleAdmin, PermInvoiceReadOwn, false},
		{RoleUser, PermCustomerReadOwn, true},
		{RoleUser, PermPackageRead, true},
		{RoleUser, PermPPPoEReadOwn, true},
		{RoleUser, PermInvoiceReadOwn, true},
		{RoleUser, PermPaymentWriteOwn, true},
		{RoleUser, PermSessionReadOwn, true},
		{RoleUser, PermCustomerRead, false},
		{RoleUser, PermCustomerWrite, false},
		{RoleUser, PermInvoiceWrite, false},
		{RoleUser, PermSettingsWrite, false},
		{RoleUser, PermAuditRead, false},
		{"ghost", PermSessionRead, false},
		{RoleAdmin, "nope:read", false},
	}
	for _, tt := range tests {
		if got := HasPermission(tt.role, tt.perm); got != tt.want {
			t.Errorf("HasPermission(%q,%q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestPermissionsFor(t *testing.T) {
	if got := len(PermissionsFor(RoleAdmin)); got != 14 {
		t.Fatalf("admin permissions = %d, want 14", got)
	}
	if got := len(PermissionsFor(RoleUser)); got != 6 {
		t.Fatalf("user permissions = %d, want 6", got)
	}
	if got := PermissionsFor("ghost"); len(got) != 0 {
		t.Fatalf("unknown role permissions = %v, want empty", got)
	}
}
