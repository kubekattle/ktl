package stack

import (
	"os"
	"os/user"
	"strconv"
	"strings"
)

func defaultLockOwner() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "unknown-host"
	}
	pid := os.Getpid()

	u, _ := user.Current()
	if u != nil && strings.TrimSpace(u.Username) != "" {
		return strings.TrimSpace(u.Username) + "@" + host + ":" + strconv.Itoa(pid)
	}
	return host + ":" + strconv.Itoa(pid)
}
