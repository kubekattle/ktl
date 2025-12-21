//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectUserNS() string {
	if v, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil {
		trimmed := strings.TrimSpace(string(v))
		switch trimmed {
		case "1":
			return "available (unprivileged_userns_clone=1)"
		case "0":
			return "disabled (unprivileged_userns_clone=0)"
		default:
			return "unknown (unprivileged_userns_clone=" + trimmed + ")"
		}
	}
	if commandExists("unshare") {
		cmd := exec.Command("unshare", "-Ur", "true")
		if err := cmd.Run(); err == nil {
			return "available (unshare -Ur ok)"
		}
	}
	if _, err := os.Stat("/proc/self/ns/user"); err == nil {
		return "present"
	}
	return "unknown"
}

func detectCgroup() string {
	root := "/sys/fs/cgroup"
	if _, err := os.Stat(root); err != nil {
		return "missing"
	}
	if _, err := os.Stat(filepath.Join(root, "cgroup.controllers")); err == nil {
		return "v2"
	}
	return "v1/unknown"
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
