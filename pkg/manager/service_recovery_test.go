package manager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func newRecoveryTestManager() *Manager {
	return &Manager{
		log:                   NewNopLogger(),
		events:                NewEventEmitter(),
		eventCh:               make(chan internalEvent, 8),
		scheduledTimers:       make(map[*time.Timer]struct{}),
		modemResetDedupWindow: defaultModemResetDedupWindow,
		uimRecoverCooldown:    defaultUIMRecoverCooldown,
	}
}

func waitInternalRecoveryEvent(t *testing.T, ch <-chan internalEvent, timeout time.Duration) internalEvent {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for internal event after %s", timeout)
		return 0
	}
}

func recoverableQMIError(service uint8, msg uint16) error {
	return &qmi.QMIError{
		Service:   service,
		MessageID: msg,
		Result:    0x0001,
		ErrorCode: qmi.QMIErrInvalidID,
	}
}

func ctlGetClientIDFailure() error {
	return &qmi.QMIError{
		Service:   qmi.ServiceControl,
		MessageID: qmi.CTLGetClientID,
		Result:    0x0001,
		ErrorCode: qmi.QMIErrClientIDsExhausted,
	}
}

func TestDMSRecoveryRebindThenRetrySuccess(t *testing.T) {
	m := newRecoveryTestManager()
	m.ensureDMSServiceHook = func() (*qmi.DMSService, error) { return &qmi.DMSService{}, nil }

	rebindCalls := 0
	m.rebindDMSServiceHook = func(reason string) (*qmi.DMSService, error) {
		rebindCalls++
		if reason != "recover:GetDeviceSerialNumbers" {
			t.Fatalf("unexpected rebind reason: %q", reason)
		}
		return &qmi.DMSService{}, nil
	}

	attempts := 0
	err := m.withDMSRecovery("GetDeviceSerialNumbers", func(dms *qmi.DMSService) error {
		attempts++
		if attempts == 1 {
			return recoverableQMIError(qmi.ServiceDMS, 0x0025)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if rebindCalls != 1 {
		t.Fatalf("expected 1 rebind call, got %d", rebindCalls)
	}
}

func TestNASRecoveryRebindThenRetrySuccess(t *testing.T) {
	m := newRecoveryTestManager()
	m.ensureNASServiceHook = func() (*qmi.NASService, error) { return &qmi.NASService{}, nil }

	rebindCalls := 0
	m.rebindNASServiceHook = func(reason string) (*qmi.NASService, error) {
		rebindCalls++
		if reason != "recover:GetServingSystem" {
			t.Fatalf("unexpected rebind reason: %q", reason)
		}
		return &qmi.NASService{}, nil
	}

	attempts := 0
	_, err := withNASRecoveryValue(m, "GetServingSystem", func(nas *qmi.NASService) (struct{}, error) {
		attempts++
		if attempts == 1 {
			return struct{}{}, recoverableQMIError(qmi.ServiceNAS, qmi.NASGetServingSystem)
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if rebindCalls != 1 {
		t.Fatalf("expected 1 rebind call, got %d", rebindCalls)
	}
}

func TestVOICERecoveryRebindThenRetrySuccess(t *testing.T) {
	m := newRecoveryTestManager()
	m.ensureVOICEServiceHook = func() (*qmi.VOICEService, error) { return &qmi.VOICEService{}, nil }

	rebindCalls := 0
	m.rebindVOICEServiceHook = func(reason string) (*qmi.VOICEService, error) {
		rebindCalls++
		if reason != "recover:VOICEGetConfig" {
			t.Fatalf("unexpected rebind reason: %q", reason)
		}
		return &qmi.VOICEService{}, nil
	}

	attempts := 0
	_, err := withVOICERecoveryValue(m, "VOICEGetConfig", func(voice *qmi.VOICEService) (struct{}, error) {
		attempts++
		if attempts == 1 {
			return struct{}{}, recoverableQMIError(qmi.ServiceVOICE, qmi.VOICEGetConfig)
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if rebindCalls != 1 {
		t.Fatalf("expected 1 rebind call, got %d", rebindCalls)
	}
}

func TestWMSRecoveryRebindThenRetrySuccessAndReplay(t *testing.T) {
	m := newRecoveryTestManager()
	m.ensureWMSServiceHook = func() (*qmi.WMSService, error) { return &qmi.WMSService{}, nil }

	rebindCalls := 0
	m.rebindWMSServiceHook = func(reason string) (*qmi.WMSService, error) {
		rebindCalls++
		if reason != "recover:WMSGetRoutes" {
			t.Fatalf("unexpected rebind reason: %q", reason)
		}
		return &qmi.WMSService{}, nil
	}

	// Force replay path to run but fail softly, verifying it does not block main retry success.
	eventReportCalls := 0
	indicationCalls := 0
	smscCalls := 0
	m.registerWMSEventReport = func(_ context.Context) error {
		eventReportCalls++
		return fmt.Errorf("forced register event report failure")
	}
	m.registerWMSIndications = func(_ context.Context, _ bool) error {
		indicationCalls++
		return fmt.Errorf("forced indication register failure")
	}
	m.queryWMSRoutes = func(_ context.Context) (*qmi.WMSRouteConfig, error) {
		return nil, fmt.Errorf("forced routes query failure")
	}
	m.queryWMSTransportState = func(_ context.Context) (qmi.WMSTransportNetworkRegistration, error) {
		return 0, fmt.Errorf("forced transport query failure")
	}
	m.querySMSC = func(_ context.Context) (string, error) {
		smscCalls++
		return "", fmt.Errorf("forced smsc query failure")
	}

	attempts := 0
	err := m.withWMSRecovery("WMSGetRoutes", func(wms *qmi.WMSService) error {
		attempts++
		if attempts == 1 {
			return recoverableQMIError(qmi.ServiceWMS, qmi.WMSGetRoutes)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if rebindCalls != 1 {
		t.Fatalf("expected 1 rebind call, got %d", rebindCalls)
	}
	// replay hook path with forced failures should still execute
	if eventReportCalls == 0 && indicationCalls == 0 {
		t.Fatal("expected WMS replay path to run after rebind")
	}
	if smscCalls == 0 {
		t.Fatal("expected WMS replay to refresh SMSC diagnostics after rebind")
	}
}

func TestServiceRecoveryRebindFailureTriggersCoreRecovery(t *testing.T) {
	type tc struct {
		name   string
		invoke func(m *Manager) error
	}

	cases := []tc{
		{
			name: "DMS",
			invoke: func(m *Manager) error {
				m.ensureDMSServiceHook = func() (*qmi.DMSService, error) { return &qmi.DMSService{}, nil }
				m.rebindDMSServiceHook = func(reason string) (*qmi.DMSService, error) { return nil, ctlGetClientIDFailure() }
				return m.withDMSRecovery("DMS.Op", func(dms *qmi.DMSService) error {
					return recoverableQMIError(qmi.ServiceDMS, 0x0025)
				})
			},
		},
		{
			name: "NAS",
			invoke: func(m *Manager) error {
				m.ensureNASServiceHook = func() (*qmi.NASService, error) { return &qmi.NASService{}, nil }
				m.rebindNASServiceHook = func(reason string) (*qmi.NASService, error) { return nil, ctlGetClientIDFailure() }
				return m.withNASRecovery("NAS.Op", func(nas *qmi.NASService) error {
					return recoverableQMIError(qmi.ServiceNAS, qmi.NASGetServingSystem)
				})
			},
		},
		{
			name: "WMS",
			invoke: func(m *Manager) error {
				m.ensureWMSServiceHook = func() (*qmi.WMSService, error) { return &qmi.WMSService{}, nil }
				m.rebindWMSServiceHook = func(reason string) (*qmi.WMSService, error) { return nil, ctlGetClientIDFailure() }
				return m.withWMSRecovery("WMS.Op", func(wms *qmi.WMSService) error {
					return recoverableQMIError(qmi.ServiceWMS, qmi.WMSGetRoutes)
				})
			},
		},
		{
			name: "VOICE",
			invoke: func(m *Manager) error {
				m.ensureVOICEServiceHook = func() (*qmi.VOICEService, error) { return &qmi.VOICEService{}, nil }
				m.rebindVOICEServiceHook = func(reason string) (*qmi.VOICEService, error) { return nil, ctlGetClientIDFailure() }
				return m.withVOICERecovery("VOICE.Op", func(voice *qmi.VOICEService) error {
					return recoverableQMIError(qmi.ServiceVOICE, qmi.VOICEGetConfig)
				})
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newRecoveryTestManager()
			m.coreReady = true
			m.state = StateDisconnected
			err := c.invoke(m)
			if err == nil {
				t.Fatal("expected error when rebind fails")
			}
			evt := waitInternalRecoveryEvent(t, m.eventCh, time.Second)
			if evt != eventModemReset {
				t.Fatalf("expected eventModemReset, got %v", evt)
			}
		})
	}
}

func TestServiceRecoveryRetryFailureTriggersCoreRecovery(t *testing.T) {
	type tc struct {
		name   string
		invoke func(m *Manager) (int, error)
	}

	cases := []tc{
		{
			name: "DMS",
			invoke: func(m *Manager) (int, error) {
				attempts := 0
				m.ensureDMSServiceHook = func() (*qmi.DMSService, error) { return &qmi.DMSService{}, nil }
				m.rebindDMSServiceHook = func(reason string) (*qmi.DMSService, error) { return &qmi.DMSService{}, nil }
				err := m.withDMSRecovery("DMS.Op", func(dms *qmi.DMSService) error {
					attempts++
					return &qmi.QMIError{Service: qmi.ServiceDMS, MessageID: 0x0025, Result: 0x0001, ErrorCode: qmi.QMIErrDeviceNotReady}
				})
				return attempts, err
			},
		},
		{
			name: "NAS",
			invoke: func(m *Manager) (int, error) {
				attempts := 0
				m.ensureNASServiceHook = func() (*qmi.NASService, error) { return &qmi.NASService{}, nil }
				m.rebindNASServiceHook = func(reason string) (*qmi.NASService, error) { return &qmi.NASService{}, nil }
				err := m.withNASRecovery("NAS.Op", func(nas *qmi.NASService) error {
					attempts++
					return &qmi.QMIError{Service: qmi.ServiceNAS, MessageID: qmi.NASGetServingSystem, Result: 0x0001, ErrorCode: qmi.QMIErrDeviceNotReady}
				})
				return attempts, err
			},
		},
		{
			name: "WMS",
			invoke: func(m *Manager) (int, error) {
				attempts := 0
				m.ensureWMSServiceHook = func() (*qmi.WMSService, error) { return &qmi.WMSService{}, nil }
				m.rebindWMSServiceHook = func(reason string) (*qmi.WMSService, error) { return &qmi.WMSService{}, nil }
				m.onWMSRebindReplayHook = func(reason string) {}
				err := m.withWMSRecovery("WMS.Op", func(wms *qmi.WMSService) error {
					attempts++
					return &qmi.QMIError{Service: qmi.ServiceWMS, MessageID: qmi.WMSGetRoutes, Result: 0x0001, ErrorCode: qmi.QMIErrDeviceNotReady}
				})
				return attempts, err
			},
		},
		{
			name: "VOICE",
			invoke: func(m *Manager) (int, error) {
				attempts := 0
				m.ensureVOICEServiceHook = func() (*qmi.VOICEService, error) { return &qmi.VOICEService{}, nil }
				m.rebindVOICEServiceHook = func(reason string) (*qmi.VOICEService, error) { return &qmi.VOICEService{}, nil }
				err := m.withVOICERecovery("VOICE.Op", func(voice *qmi.VOICEService) error {
					attempts++
					return &qmi.QMIError{Service: qmi.ServiceVOICE, MessageID: qmi.VOICEGetConfig, Result: 0x0001, ErrorCode: qmi.QMIErrDeviceNotReady}
				})
				return attempts, err
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newRecoveryTestManager()
			m.coreReady = true
			m.state = StateDisconnected
			attempts, err := c.invoke(m)
			if err == nil {
				t.Fatal("expected retry failure")
			}
			if attempts != 2 {
				t.Fatalf("expected 2 attempts, got %d", attempts)
			}
			evt := waitInternalRecoveryEvent(t, m.eventCh, time.Second)
			if evt != eventModemReset {
				t.Fatalf("expected eventModemReset, got %v", evt)
			}
		})
	}
}

func TestServiceRecoveryCooldownSuppressesRepeatedCoreRecovery(t *testing.T) {
	m := newRecoveryTestManager()
	m.coreReady = true
	m.state = StateDisconnected
	m.uimRecoverCooldown = time.Hour

	m.ensureDMSServiceHook = func() (*qmi.DMSService, error) { return &qmi.DMSService{}, nil }
	m.rebindDMSServiceHook = func(reason string) (*qmi.DMSService, error) { return nil, ctlGetClientIDFailure() }
	_ = m.withDMSRecovery("DMS.Op", func(dms *qmi.DMSService) error {
		return recoverableQMIError(qmi.ServiceDMS, 0x0025)
	})
	if evt := waitInternalRecoveryEvent(t, m.eventCh, time.Second); evt != eventModemReset {
		t.Fatalf("expected first event to be modem reset, got %v", evt)
	}

	m.ensureNASServiceHook = func() (*qmi.NASService, error) { return &qmi.NASService{}, nil }
	m.rebindNASServiceHook = func(reason string) (*qmi.NASService, error) { return nil, ctlGetClientIDFailure() }
	_ = m.withNASRecovery("NAS.Op", func(nas *qmi.NASService) error {
		return recoverableQMIError(qmi.ServiceNAS, qmi.NASGetServingSystem)
	})

	select {
	case evt := <-m.eventCh:
		t.Fatalf("expected cooldown to suppress second recovery event, got %v", evt)
	case <-time.After(350 * time.Millisecond):
	}
}

func TestServiceRecoveryEnsureHooks(t *testing.T) {
	m := newRecoveryTestManager()

	dmsEnsureCalls := 0
	nasEnsureCalls := 0
	wmsEnsureCalls := 0
	voiceEnsureCalls := 0
	m.ensureDMSServiceHook = func() (*qmi.DMSService, error) {
		dmsEnsureCalls++
		return &qmi.DMSService{}, nil
	}
	m.ensureNASServiceHook = func() (*qmi.NASService, error) {
		nasEnsureCalls++
		return &qmi.NASService{}, nil
	}
	m.ensureWMSServiceHook = func() (*qmi.WMSService, error) {
		wmsEnsureCalls++
		return &qmi.WMSService{}, nil
	}
	m.ensureVOICEServiceHook = func() (*qmi.VOICEService, error) {
		voiceEnsureCalls++
		return &qmi.VOICEService{}, nil
	}

	if err := m.withDMSRecovery("DMS.Ensure", func(dms *qmi.DMSService) error { return nil }); err != nil {
		t.Fatalf("DMS ensure path failed: %v", err)
	}
	if err := m.withNASRecovery("NAS.Ensure", func(nas *qmi.NASService) error { return nil }); err != nil {
		t.Fatalf("NAS ensure path failed: %v", err)
	}
	if err := m.withWMSRecovery("WMS.Ensure", func(wms *qmi.WMSService) error { return nil }); err != nil {
		t.Fatalf("WMS ensure path failed: %v", err)
	}
	if err := m.withVOICERecovery("VOICE.Ensure", func(voice *qmi.VOICEService) error { return nil }); err != nil {
		t.Fatalf("VOICE ensure path failed: %v", err)
	}

	if dmsEnsureCalls != 1 || nasEnsureCalls != 1 || wmsEnsureCalls != 1 || voiceEnsureCalls != 1 {
		t.Fatalf(
			"unexpected ensure hook calls: dms=%d nas=%d wms=%d voice=%d",
			dmsEnsureCalls, nasEnsureCalls, wmsEnsureCalls, voiceEnsureCalls,
		)
	}
}

func TestEnqueueModemResetEventDeduplicatesBurst(t *testing.T) {
	m := newRecoveryTestManager()
	m.modemResetDedupWindow = time.Minute

	m.enqueueModemResetEvent("t1")
	m.enqueueModemResetEvent("t2")

	if evt := waitInternalRecoveryEvent(t, m.eventCh, time.Second); evt != eventModemReset {
		t.Fatalf("expected modem reset event, got %v", evt)
	}

	select {
	case evt := <-m.eventCh:
		t.Fatalf("expected second event to be deduplicated, got %v", evt)
	case <-time.After(300 * time.Millisecond):
	}

	stats := m.Stats()
	if stats.ResetEvents != 2 {
		t.Fatalf("expected reset_events=2, got %d", stats.ResetEvents)
	}
	if stats.ResetCoalesced < 1 {
		t.Fatalf("expected reset_coalesced >= 1, got %d", stats.ResetCoalesced)
	}
}

func TestHandleModemResetEventCoalescesWhileRecovering(t *testing.T) {
	m := newRecoveryTestManager()
	m.modemResetRecovering = true

	m.handleEvent(eventModemReset)

	select {
	case evt := <-m.eventCh:
		t.Fatalf("unexpected modem reset enqueue while recovering: %v", evt)
	case <-time.After(200 * time.Millisecond):
	}

	if !m.modemResetPending {
		t.Fatal("expected modemResetPending=true while coalescing")
	}
	if stats := m.Stats(); stats.ResetCoalesced < 1 {
		t.Fatalf("expected reset_coalesced >= 1, got %d", stats.ResetCoalesced)
	}
}
