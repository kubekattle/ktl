package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func remoteTransportCredentials(cmd *cobra.Command, target string) (credentials.TransportCredentials, error) {
	enabled, caPath, serverName, skipVerify, clientCert, clientKey, err := remoteTLSConfig(cmd)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return insecure.NewCredentials(), nil
	}
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: skipVerify,
	}
	if strings.TrimSpace(clientCert) != "" || strings.TrimSpace(clientKey) != "" {
		if strings.TrimSpace(clientCert) == "" || strings.TrimSpace(clientKey) == "" {
			return nil, fmt.Errorf("both --remote-tls-client-cert and --remote-tls-client-key are required for mTLS")
		}
		cert, err := tls.LoadX509KeyPair(strings.TrimSpace(clientCert), strings.TrimSpace(clientKey))
		if err != nil {
			return nil, fmt.Errorf("load remote TLS client keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	if strings.TrimSpace(serverName) == "" {
		// Default server name to the host part of "host:port" to make TLS usable
		// with grpcutil's passthrough target rewriting.
		if host, _, err := net.SplitHostPort(strings.TrimSpace(target)); err == nil && host != "" {
			serverName = host
		}
	}
	if strings.TrimSpace(serverName) != "" {
		tlsCfg.ServerName = strings.TrimSpace(serverName)
	}
	if !skipVerify {
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if strings.TrimSpace(caPath) != "" {
			raw, err := os.ReadFile(strings.TrimSpace(caPath))
			if err != nil {
				return nil, fmt.Errorf("read remote TLS CA: %w", err)
			}
			if !pool.AppendCertsFromPEM(raw) {
				return nil, fmt.Errorf("parse remote TLS CA PEM from %q", caPath)
			}
		}
		tlsCfg.RootCAs = pool
	}
	return credentials.NewTLS(tlsCfg), nil
}

func remoteTLSConfig(cmd *cobra.Command) (enabled bool, caPath string, serverName string, insecureSkipVerify bool, clientCert string, clientKey string, _ error) {
	root := cmd
	if root != nil {
		root = cmd.Root()
	}
	if root != nil {
		if f := root.PersistentFlags().Lookup("remote-tls"); f != nil {
			enabled = enabled || strings.EqualFold(strings.TrimSpace(f.Value.String()), "true")
		}
		if f := root.PersistentFlags().Lookup("remote-tls-ca"); f != nil {
			caPath = strings.TrimSpace(f.Value.String())
		}
		if f := root.PersistentFlags().Lookup("remote-tls-server-name"); f != nil {
			serverName = strings.TrimSpace(f.Value.String())
		}
		if f := root.PersistentFlags().Lookup("remote-tls-insecure-skip-verify"); f != nil {
			insecureSkipVerify = insecureSkipVerify || strings.EqualFold(strings.TrimSpace(f.Value.String()), "true")
		}
		if f := root.PersistentFlags().Lookup("remote-tls-client-cert"); f != nil {
			clientCert = strings.TrimSpace(f.Value.String())
		}
		if f := root.PersistentFlags().Lookup("remote-tls-client-key"); f != nil {
			clientKey = strings.TrimSpace(f.Value.String())
		}
	}

	if v, ok, err := envBool("KTL_REMOTE_TLS"); err != nil {
		return false, "", "", false, "", "", err
	} else if ok {
		enabled = enabled || v
	}
	if caPath == "" {
		caPath = strings.TrimSpace(os.Getenv("KTL_REMOTE_TLS_CA"))
	}
	if serverName == "" {
		serverName = strings.TrimSpace(os.Getenv("KTL_REMOTE_TLS_SERVER_NAME"))
	}
	if v, ok, err := envBool("KTL_REMOTE_TLS_INSECURE_SKIP_VERIFY"); err != nil {
		return false, "", "", false, "", "", err
	} else if ok {
		insecureSkipVerify = insecureSkipVerify || v
	}
	if clientCert == "" {
		clientCert = strings.TrimSpace(os.Getenv("KTL_REMOTE_TLS_CLIENT_CERT"))
	}
	if clientKey == "" {
		clientKey = strings.TrimSpace(os.Getenv("KTL_REMOTE_TLS_CLIENT_KEY"))
	}

	if !enabled && (caPath != "" || serverName != "" || insecureSkipVerify || clientCert != "" || clientKey != "") {
		enabled = true
	}
	return enabled, caPath, serverName, insecureSkipVerify, clientCert, clientKey, nil
}
