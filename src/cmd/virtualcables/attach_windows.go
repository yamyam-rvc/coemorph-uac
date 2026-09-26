//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var usbipPortPattern = regexp.MustCompile(`(?i)^Port\s+(\d+):`)
var ownUSBIPURLPattern = regexp.MustCompile(`(?i)usbip://127\.0\.0\.1:3240/(1-\d+)(?:\s|$)`)
var usbipReceiveModePattern = regexp.MustCompile(`(?i)(?:^|->\s*)mode:\s*(\S+)`)

// A warm restart does not need another attachment when all of this app's
// cables are already attached with the requested receive mode.
func ownCableAttachmentsMatch(output string, count int, receiveMode string) bool {
	if count < 1 || count > 32 {
		return false
	}
	attached := make(map[string]string)
	portInUse := false
	currentBus := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if usbipPortPattern.MatchString(trimmed) {
			portInUse = strings.Contains(strings.ToLower(trimmed), "device in use")
			currentBus = ""
			continue
		}
		if !portInUse {
			continue
		}
		if match := ownUSBIPURLPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			currentBus = match[1]
			if _, duplicate := attached[currentBus]; duplicate {
				return false
			}
			attached[currentBus] = ""
			continue
		}
		if currentBus != "" {
			if match := usbipReceiveModePattern.FindStringSubmatch(trimmed); len(match) == 2 {
				attached[currentBus] = match[1]
				currentBus = ""
			}
		}
	}
	if len(attached) != count {
		return false
	}
	for number := 1; number <= count; number++ {
		if attached["1-"+strconv.Itoa(number)] != receiveMode {
			return false
		}
	}
	return true
}

func existingCablesAttached(executable string, count int, receiveMode string, logger *log.Logger) bool {
	output, err := runUSBIP(executable, logger, "port")
	return err == nil && ownCableAttachmentsMatch(output, count, receiveMode)
}

func usbipAttachArguments(busID, receiveMode string) ([]string, error) {
	args := []string{"attach", "-r", "127.0.0.1", "-b", busID}
	switch receiveMode {
	case "zero-copy": // preserve the upstream/default attach behavior
	case "low-latency":
		args = append(args, "--receive-mode=low-latency")
	default:
		return nil, fmt.Errorf("invalid USB/IP receive mode %q", receiveMode)
	}
	return args, nil
}

func findUSBIPExecutable() string {
	if path, err := exec.LookPath("usbip.exe"); err == nil {
		return path
	}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "USBip", "usbip.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "usbip-win2", "usbip.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "usbip", "usbip.exe"),
	}
	if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
		candidates = append(candidates,
			filepath.Join(programFilesX86, "USBip", "usbip.exe"),
			filepath.Join(programFilesX86, "usbip-win2", "usbip.exe"),
		)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func runUSBIP(executable string, logger *log.Logger, arguments ...string) (string, error) {
	logger.Printf("usbip.exe %s", strings.Join(arguments, " "))
	command := exec.Command(executable, arguments...)
	hideCommandWindow(command)
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if text != "" {
		logger.Printf("usbip: %s", strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", " | "), "\n", " | "))
	}
	if err != nil {
		return text, fmt.Errorf("usbip %s: %w", strings.Join(arguments, " "), err)
	}
	return text, nil
}

func hideCommandWindow(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

func detachOwnUSBIPPorts(executable string, logger *log.Logger) {
	output, err := runUSBIP(executable, logger, "port")
	if err != nil {
		logger.Printf("Could not inspect existing USB/IP ports: %v", err)
		return
	}
	currentPort := ""
	var ownPorts []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if match := usbipPortPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			currentPort = match[1]
			continue
		}
		if currentPort != "" && strings.Contains(trimmed, "usbip://127.0.0.1:3240/") {
			ownPorts = append(ownPorts, currentPort)
			currentPort = ""
		}
	}
	for _, port := range ownPorts {
		if _, err := runUSBIP(executable, logger, "detach", "-p", port); err != nil {
			logger.Printf("Could not detach previous Coemorph UAC Lab port %s: %v", port, err)
		}
	}
}

func runAttachHelper(count int, logger *log.Logger) error {
	if count < 1 || count > 32 {
		return fmt.Errorf("invalid cable count %d", count)
	}
	executable := findUSBIPExecutable()
	if executable == "" {
		return fmt.Errorf("usbip.exe was not found; install the driver from the main window")
	}
	logger.Printf("Windows device synchronization started for %d cable(s)", count)
	logger.Printf("usbip.exe: %s", executable)
	logger.Printf("USB/IP receive mode: %s", configuredUSBIPReceiveMode)

	// The server can still be completing its restart when the elevated helper
	// starts. Retry only the local device-list request for a short bounded time.
	var listErr error
	for attempt := 1; attempt <= 20; attempt++ {
		if _, listErr = runUSBIP(executable, logger, "list", "-r", "127.0.0.1"); listErr == nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if listErr != nil {
		return fmt.Errorf("local USB/IP server is not ready: %w", listErr)
	}

	detachOwnUSBIPPorts(executable, logger)
	for number := 1; number <= count; number++ {
		busID := "1-" + strconv.Itoa(number)
		args, err := usbipAttachArguments(busID, configuredUSBIPReceiveMode)
		if err != nil {
			return err
		}
		if _, err := runUSBIP(executable, logger, args...); err != nil {
			return fmt.Errorf("attach %s failed: %w", busID, err)
		}
	}
	logger.Printf("Windows device synchronization completed for %d cable(s)", count)
	return nil
}
