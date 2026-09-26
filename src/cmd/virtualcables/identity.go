package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const installationIDLength = 32

func installationIDPath() string {
	return filepath.Join(applicationDataDir(), "INSTALL_ID")
}

func parseInstallationID(data []byte) (string, error) {
	id := strings.TrimSpace(string(data))
	if len(id) != installationIDLength {
		return "", fmt.Errorf("stored USB installation ID has invalid length")
	}
	for _, c := range []byte(id) {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			return "", fmt.Errorf("stored USB installation ID contains invalid characters")
		}
	}
	return id, nil
}

func readInstallationID(path string) (string, error) {
	// A second process may see the file immediately after O_EXCL creation,
	// before the first process has finished writing it.
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		data, err := os.ReadFile(path)
		if err == nil {
			id, parseErr := parseInstallationID(data)
			if parseErr == nil {
				return id, nil
			}
			lastErr = parseErr
		} else {
			if errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", fmt.Errorf("read USB installation ID %s: %w", path, lastErr)
}

func loadOrCreateInstallationID() (string, error) {
	path := installationIDPath()
	if id, err := readInstallationID(path); err == nil {
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		// Never silently replace a damaged ID: that would create a new USB
		// identity and strand the old device instance in Windows.
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create USB identity directory: %w", err)
	}
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate USB installation ID: %w", err)
	}
	id := strings.ToUpper(hex.EncodeToString(randomBytes[:]))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readInstallationID(path)
	}
	if err != nil {
		return "", fmt.Errorf("create USB installation ID: %w", err)
	}
	if _, err := file.WriteString(id + "\n"); err != nil {
		file.Close()
		return "", fmt.Errorf("write USB installation ID: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", fmt.Errorf("sync USB installation ID: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close USB installation ID: %w", err)
	}
	return id, nil
}
