package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/iniwex5/quectel-cm-go/pkg/qmi"
)

func TestHandleIndicationIMSRegistrationStatus(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{0x34, 0x12}},
			{Type: 0x11, Value: []byte{0x02, 0x00, 0x00, 0x00}},
			{Type: 0x12, Value: []byte("ok")},
			{Type: 0x13, Value: []byte{0x01, 0x00, 0x00, 0x00}},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventIMSRegistrationStatus, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventIMSRegistrationStatus {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.IMSRegistration == nil || evt.IMSRegistration.Status != qmi.IMSARegistrationStateRegistered {
		t.Fatalf("unexpected IMS registration payload: %+v", evt.IMSRegistration)
	}
}

func TestHandleIndicationIMSServicesStatus(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{0x02, 0x00, 0x00, 0x00}},
			{Type: 0x14, Value: []byte{0x01, 0x00, 0x00, 0x00}},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventIMSServicesStatus, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventIMSServicesStatus {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.IMSServices == nil || evt.IMSServices.SMSServiceStatus != qmi.IMSAServiceAvailabilityAvailable {
		t.Fatalf("unexpected IMS services payload: %+v", evt.IMSServices)
	}
}

func TestHandleIndicationIMSSettingsChanged(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{0x01}},
			{Type: 0x1E, Value: []byte{0x01}},
			{Type: 0x20, Value: []byte{0x00}},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventIMSSettingsChanged, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventIMSSettingsChanged {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.IMSSettings == nil || !evt.IMSSettings.VoiceOverLTEEnabled || !evt.IMSSettings.PresenceEnabled {
		t.Fatalf("unexpected IMS settings payload: %+v", evt.IMSSettings)
	}
}

func TestIMSAIndicationRegistrationDisabled(t *testing.T) {
	m := &Manager{cfg: Config{DisableIMSAInd: true}}
	cfg, ok := m.imsaIndicationRegistration()
	if ok {
		t.Fatalf("expected IMSA indications to be disabled, got cfg=%+v", cfg)
	}
}

func TestIMSAIndicationRegistrationEnabledDefaults(t *testing.T) {
	m := &Manager{}
	cfg, ok := m.imsaIndicationRegistration()
	if !ok {
		t.Fatal("expected IMSA indications to be enabled")
	}
	if !cfg.RegistrationStatusChanged || !cfg.ServicesStatusChanged {
		t.Fatalf("unexpected IMSA indication config: %+v", cfg)
	}
}

func TestIMSDeviceQueryReturnsServiceNotReady(t *testing.T) {
	m := &Manager{}
	var notReady *ServiceNotReadyError

	if err := m.IMSABind(context.Background(), 1); !errors.As(err, &notReady) {
		t.Fatalf("expected IMSABind to return ServiceNotReadyError, got %v", err)
	}

	if _, err := m.IMSGetServicesEnabledSetting(context.Background()); !errors.As(err, &notReady) {
		t.Fatalf("expected IMSGetServicesEnabledSetting to return ServiceNotReadyError, got %v", err)
	}

	if _, err := m.IMSPGetEnablerState(context.Background()); !errors.As(err, &notReady) {
		t.Fatalf("expected IMSPGetEnablerState to return ServiceNotReadyError, got %v", err)
	}
}
