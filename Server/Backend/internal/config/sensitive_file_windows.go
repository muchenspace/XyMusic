//go:build windows

package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	windowsSIDOnce sync.Once
	windowsSID     string
	windowsSIDErr  error
	sidPattern     = regexp.MustCompile(`S-\d+(?:-\d+)+`)
)

func protectSensitiveFile(path string) error {
	sid, err := currentWindowsUserSID()
	if err != nil {
		return errors.New("sensitive path permissions could not be restricted")
	}
	icacls := windowsSystemExecutable("icacls.exe")
	commands := [][]string{
		{path, "/reset"},
		{path, "/inheritance:r"},
		{path, "/grant:r", "*" + sid + ":(F)", "*S-1-5-18:(F)", "*S-1-5-32-544:(F)"},
	}
	for _, arguments := range commands {
		if err := runSensitiveWindowsCommand(icacls, arguments...); err != nil {
			return errors.New("sensitive path permissions could not be restricted")
		}
	}
	return nil
}

func currentWindowsUserSID() (string, error) {
	windowsSIDOnce.Do(func() {
		output, err := runSensitiveWindowsCommandOutput(
			windowsSystemExecutable("whoami.exe"), "/user", "/fo", "csv", "/nh",
		)
		if err != nil {
			windowsSIDErr = err
			return
		}
		windowsSID = sidPattern.FindString(output)
		if windowsSID == "" {
			windowsSIDErr = errors.New("current Windows user SID is unavailable")
		}
	})
	return windowsSID, windowsSIDErr
}

func runSensitiveWindowsCommand(executable string, arguments ...string) error {
	_, err := runSensitiveWindowsCommandOutput(executable, arguments...)
	return err
}

func runSensitiveWindowsCommandOutput(executable string, arguments ...string) (string, error) {
	command := exec.Command(executable, arguments...)
	command.Env = safeWindowsCommandEnvironment()
	command.Stdin = nil
	output, err := command.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func safeWindowsCommandEnvironment() []string {
	allowed := []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "ComSpec", "PATHEXT"}
	result := make([]string, 0, len(allowed))
	for _, key := range allowed {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

func windowsSystemExecutable(name string) string {
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		root = strings.TrimSpace(os.Getenv("WINDIR"))
	}
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", name)
}
