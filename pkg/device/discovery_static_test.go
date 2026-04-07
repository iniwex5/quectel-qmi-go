package device

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeATPorts(t *testing.T) {
	got := normalizeATPorts([]string{
		" /dev/ttyUSB7 ",
		"/dev/ttyUSB6",
		"",
		"/dev/ttyUSB7",
		"/dev/ttyUSB4",
	})

	want := []string{"/dev/ttyUSB4", "/dev/ttyUSB6", "/dev/ttyUSB7"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want=%q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestChooseStaticATPortsPrefersHintWithinDevicePorts(t *testing.T) {
	primary, backup := chooseStaticATPorts(
		[]string{"/dev/ttyUSB6", "/dev/ttyUSB7", "/dev/ttyUSB4"},
		"/dev/ttyUSB7",
	)

	if primary != "/dev/ttyUSB7" {
		t.Fatalf("primary=%q want=%q", primary, "/dev/ttyUSB7")
	}
	if backup != "/dev/ttyUSB4" {
		t.Fatalf("backup=%q want=%q", backup, "/dev/ttyUSB4")
	}
}

func TestChooseStaticATPortsIgnoresHintOutsideDevicePorts(t *testing.T) {
	primary, backup := chooseStaticATPorts(
		[]string{"/dev/ttyUSB6", "/dev/ttyUSB7"},
		"/dev/ttyUSB4",
	)

	if primary != "/dev/ttyUSB6" {
		t.Fatalf("primary=%q want=%q", primary, "/dev/ttyUSB6")
	}
	if backup != "/dev/ttyUSB7" {
		t.Fatalf("backup=%q want=%q", backup, "/dev/ttyUSB7")
	}
}

func TestFindATPortsCollectsBothTTYLayouts(t *testing.T) {
	usbPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.2", "ttyUSB6"), 0o755); err != nil {
		t.Fatalf("mkdir direct tty layout: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usbPath, "1-2:1.3", "tty", "ttyUSB7"), 0o755); err != nil {
		t.Fatalf("mkdir nested tty layout: %v", err)
	}

	got := findATPorts(usbPath)
	want := []string{"/dev/ttyUSB6", "/dev/ttyUSB7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findATPorts()=%v want=%v", got, want)
	}
}
