package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func defaultRequester() string {
	user := strings.TrimSpace(os.Getenv("USER"))
	if user == "" {
		user = strings.TrimSpace(os.Getenv("USERNAME"))
	}
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case host != "":
		return host
	case user != "":
		return user
	default:
		return "ktl"
	}
}

func newSessionID(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "session"
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
}
