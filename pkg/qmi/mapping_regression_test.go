package qmi

import "testing"

func TestUIMMessageIDMappingsMatchSpec(t *testing.T) {
	if UIMSetPINProtection != 0x0025 {
		t.Fatalf("UIMSetPINProtection = 0x%04X, want 0x0025", UIMSetPINProtection)
	}
	if UIMVerifyPIN != 0x0026 {
		t.Fatalf("UIMVerifyPIN = 0x%04X, want 0x0026", UIMVerifyPIN)
	}
	if UIMUnblockPIN != 0x0027 {
		t.Fatalf("UIMUnblockPIN = 0x%04X, want 0x0027", UIMUnblockPIN)
	}
	if UIMChangePIN != 0x0028 {
		t.Fatalf("UIMChangePIN = 0x%04X, want 0x0028", UIMChangePIN)
	}
}

func TestDMSUIMMessageIDMappingsMatchSpec(t *testing.T) {
	if DMSUIMSetPINProtection != 0x0027 {
		t.Fatalf("DMSUIMSetPINProtection = 0x%04X, want 0x0027", DMSUIMSetPINProtection)
	}
	if DMSUIMVerifyPIN != 0x0028 {
		t.Fatalf("DMSUIMVerifyPIN = 0x%04X, want 0x0028", DMSUIMVerifyPIN)
	}
	if DMSUIMUnblockPIN != 0x0029 {
		t.Fatalf("DMSUIMUnblockPIN = 0x%04X, want 0x0029", DMSUIMUnblockPIN)
	}
	if DMSUIMChangePIN != 0x002A {
		t.Fatalf("DMSUIMChangePIN = 0x%04X, want 0x002A", DMSUIMChangePIN)
	}
}

func TestWDSProfileMessageIDMappingsMatchSpec(t *testing.T) {
	if WDSCreateProfile != 0x0027 {
		t.Fatalf("WDSCreateProfile = 0x%04X, want 0x0027", WDSCreateProfile)
	}
	if WDSModifyProfileSettings != 0x0028 {
		t.Fatalf("WDSModifyProfileSettings = 0x%04X, want 0x0028", WDSModifyProfileSettings)
	}
}
