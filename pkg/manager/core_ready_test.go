package manager

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDoRecoverFromModemResetResetsSnapshotImmediately(t *testing.T) {
	m := newRecoveryTestManager()
	m.snapshot.UpdateIdentities(DeviceIdentities{ICCID: "old-iccid", IMSI: "old-imsi"})
	m.openClientAndAllocateServicesHook = func() error { return nil }
	m.checkSIMHook = func() error { return nil }
	m.modemResetQuietWindow = 5 * time.Millisecond
	m.getICCIDStrictHook = func(ctx context.Context) (string, error) { return "new-iccid", nil }

	if ok := m.doRecoverFromModemReset(); !ok {
		t.Fatal("expected recover to succeed")
	}

	ids, _ := m.snapshot.Identities()
	if ids.ICCID != "" || ids.IMSI != "" {
		t.Fatalf("expected snapshot SIM identities reset, got iccid=%q imsi=%q", ids.ICCID, ids.IMSI)
	}
	if !m.IsCoreReady() {
		t.Fatal("expected coreReady=true after convergence")
	}
}

func TestDoRecoverFromModemResetIdentityGateBlocksCoreReady(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.Timeouts.SIMCheck = 80 * time.Millisecond
	m.openClientAndAllocateServicesHook = func() error { return nil }
	m.checkSIMHook = func() error { return nil }
	m.modemResetQuietWindow = 5 * time.Millisecond
	m.getICCIDStrictHook = func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("uim not ready")
	}
	m.getIMSIStrictHook = func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("uim not ready")
	}

	if ok := m.doRecoverFromModemReset(); ok {
		t.Fatal("expected recover to fail when identities are unreadable")
	}
	if m.IsCoreReady() {
		t.Fatal("coreReady should stay false while identity gate is unsatisfied")
	}

	m.mu.RLock()
	stage := m.coreReadyStage
	m.mu.RUnlock()
	if stage != "recover_wait_identity" {
		t.Fatalf("expected stage recover_wait_identity, got %q", stage)
	}
}

func TestDoRecoverFromModemResetPendingStormBlocksCoreReady(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.Timeouts.SIMCheck = 100 * time.Millisecond
	m.openClientAndAllocateServicesHook = func() error { return nil }
	m.checkSIMHook = func() error { return nil }
	m.modemResetQuietWindow = 100 * time.Millisecond
	m.modemResetPending = true

	if ok := m.doRecoverFromModemReset(); ok {
		t.Fatal("expected recover to fail when reset storm is still pending")
	}
	if m.IsCoreReady() {
		t.Fatal("coreReady should stay false during reset storm")
	}

	m.mu.RLock()
	stage := m.coreReadyStage
	m.mu.RUnlock()
	if stage != "recover_wait_reset_quiet" {
		t.Fatalf("expected stage recover_wait_reset_quiet, got %q", stage)
	}
}

func TestWaitCoreReadyTimeoutIncludesConvergenceContext(t *testing.T) {
	m := newRecoveryTestManager()
	m.mu.Lock()
	m.markCoreNotReadyLocked("recover_wait_identity", fmt.Errorf("uim not ready"))
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	err := m.WaitCoreReady(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "stage=recover_wait_identity") {
		t.Fatalf("expected stage in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "uim not ready") {
		t.Fatalf("expected last_err in error, got: %v", err)
	}
}

func TestStrictLiveIdentityBypassesSnapshot(t *testing.T) {
	m := newRecoveryTestManager()
	m.snapshot.UpdateIdentities(DeviceIdentities{ICCID: "cached-iccid", IMSI: "cached-imsi"})
	m.getICCIDStrictHook = func(ctx context.Context) (string, error) { return "live-iccid", nil }
	m.getIMSIStrictHook = func(ctx context.Context) (string, error) { return "live-imsi", nil }

	iccid, err := m.GetICCIDStrictLive(context.Background())
	if err != nil || iccid != "live-iccid" {
		t.Fatalf("GetICCIDStrictLive unexpected result: iccid=%q err=%v", iccid, err)
	}
	imsi, err := m.GetIMSIStrictLive(context.Background())
	if err != nil || imsi != "live-imsi" {
		t.Fatalf("GetIMSIStrictLive unexpected result: imsi=%q err=%v", imsi, err)
	}

	iccidViaDefault, err := m.GetICCID(context.Background())
	if err != nil || iccidViaDefault != "live-iccid" {
		t.Fatalf("GetICCID should follow strict-live path: iccid=%q err=%v", iccidViaDefault, err)
	}
	imsiViaDefault, err := m.GetIMSI(context.Background())
	if err != nil || imsiViaDefault != "live-imsi" {
		t.Fatalf("GetIMSI should follow strict-live path: imsi=%q err=%v", imsiViaDefault, err)
	}
}
