package natstransport

import (
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

const DefaultServer = natsgo.DefaultURL

type DialConfig struct {
	Server  string
	Creds   string
	NKey    string
	Timeout time.Duration
	Name    string
}

func ConnectOptions(config DialConfig) ([]natsgo.Option, error) {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	name := strings.TrimSpace(config.Name)
	if name == "" {
		name = "torque-nats-transport"
	}
	opts := []natsgo.Option{
		natsgo.Name(name),
		natsgo.Timeout(timeout),
	}
	if creds := strings.TrimSpace(config.Creds); creds != "" {
		opts = append(opts, natsgo.UserCredentials(creds))
	}
	if nkey := strings.TrimSpace(config.NKey); nkey != "" {
		opt, err := natsgo.NkeyOptionFromSeed(nkey)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opt)
	}
	return opts, nil
}

func ServerOrDefault(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return DefaultServer
	}
	return server
}
