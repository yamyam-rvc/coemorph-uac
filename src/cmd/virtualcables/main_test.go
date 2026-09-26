package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateConfigurationDoesNotReuseStock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	stock := []byte("CABLES=9\r\n")
	if err := os.WriteFile("CONFIG.ini", stock, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadCableCount(); got != 1 {
		t.Fatalf("candidate imported stock cable count %d", got)
	}
	if err := saveCableCount(2); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "Coemorph UAC Lab", "CONFIG.ini"); configFilePath() != want {
		t.Fatalf("config path=%q, want %q", configFilePath(), want)
	}
	if got := loadCableCount(); got != 2 {
		t.Fatalf("saved candidate cable count=%d, want 2", got)
	}
	if got, err := os.ReadFile("CONFIG.ini"); err != nil || string(got) != string(stock) {
		t.Fatalf("stock configuration changed: data=%q err=%v", got, err)
	}
}
