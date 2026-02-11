package agent

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func serverCreds(cfg Config) (credentials.TransportCredentials, error) {
	certPath := strings.TrimSpace(cfg.TLSCertFile)
	keyPath := strings.TrimSpace(cfg.TLSKeyFile)
	caPath := strings.TrimSpace(cfg.TLSClientCAFile)

	if certPath == "" && keyPath == "" {
		return insecure.NewCredentials(), nil
	}
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("both TLS cert and key are required (got cert=%q key=%q)", certPath, keyPath)
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load TLS keypair: %w", err)
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if caPath != "" {
		raw, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("parse client CA PEM from %q", caPath)
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(tlsCfg), nil
}
