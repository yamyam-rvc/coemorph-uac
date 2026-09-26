package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestInstallationIDPersistsAndSeparatesCableSerials(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	first, err := loadOrCreateInstallationID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateInstallationID()
	if err != nil || first != second {
		t.Fatalf("installation ID changed: first=%q second=%q err=%v", first, second, err)
	}
	devices, err := makeDevices(2, 100, first)
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].Descriptors.Serial == devices[1].Descriptors.Serial {
		t.Fatal("two cables shared a USB serial")
	}
	for _, dev := range devices {
		if got := dev.Descriptors.Serial; len(got) != len("COEMORPH-")+installationIDLength+4 {
			t.Fatalf("invalid serial length %d for %q", len(got), got)
		}
	}

	otherRoot := t.TempDir()
	t.Setenv("LOCALAPPDATA", otherRoot)
	other, err := loadOrCreateInstallationID()
	if err != nil || other == first {
		t.Fatalf("different profile reused installation ID: first=%q other=%q err=%v", first, other, err)
	}
}

func TestInstallationIDConcurrentCreation(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	const processes = 16
	var wg sync.WaitGroup
	ids := make([]string, processes)
	errs := make([]error, processes)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = loadOrCreateInstallationID()
		}(i)
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("concurrent creation %d: id=%q err=%v; first=%q", i, ids[i], errs[i], ids[0])
		}
	}
}

func TestDamagedInstallationIDIsNotReplaced(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	path := installationIDPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateInstallationID(); err == nil {
		t.Fatal("damaged installation ID was silently replaced")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "broken\n" {
		t.Fatalf("damaged identity was changed: %q, %v", data, err)
	}
}
