// File: cmd/ktl/build_login.go
// Brief: CLI command wiring and implementation for 'build login'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/docker/cli/cli/config/types"
	dockercred "github.com/docker/docker-credential-helpers/credentials"
	"github.com/example/ktl/internal/dockerconfig"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const defaultRegistryServer = "https://index.docker.io/v1/"

var pingRegistryFn = pingRegistry

func newBuildLoginCommand(parent *buildCLIOptions) *cobra.Command {
	var opts loginOptions

	cmd := &cobra.Command{
		Use:   "login [SERVER]",
		Short: "Log in to a container registry",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return err
			}
			if len(args) == 0 {
				return nil
			}
			return validateRegistryServerArg(args[0])
		},
		Example: `  # Login to Docker Hub (interactive prompts if flags are omitted)
  ktl build login

  # Login to GHCR using stdin (recommended)
  echo "$GITHUB_TOKEN" | ktl build login ghcr.io --username your-github-username --password-stdin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.Server = args[0]
			}
			opts.AuthFile = parent.authFile
			return runBuildLogin(cmd, opts)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().VarP(&validatedStringValue{dest: &opts.Username, name: "--username", allowEmpty: false, validator: nil}, "username", "u", "Registry username")
	cmd.Flags().VarP(&validatedStringValue{dest: &opts.Password, name: "--password", allowEmpty: false, validator: nil}, "password", "p", "Registry password or token (prefer --password-stdin)")
	cmd.Flags().BoolVar(&opts.PasswordStdin, "password-stdin", false, "Read the password/token from stdin")
	decorateCommandHelp(cmd, "Login Flags")

	return cmd
}

func newBuildLogoutCommand(parent *buildCLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout [SERVER]",
		Short: "Log out of a container registry",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return err
			}
			if len(args) == 0 {
				return nil
			}
			return validateRegistryServerArg(args[0])
		},
		Example: `  # Logout of Docker Hub
  ktl build logout

  # Logout of a specific registry
  ktl build logout ghcr.io`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server := ""
			if len(args) > 0 {
				server = args[0]
			}
			return runBuildLogout(cmd, server, parent.authFile)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	decorateCommandHelp(cmd, "Logout Flags")
	return cmd
}

type loginOptions struct {
	Server        string
	Username      string
	Password      string
	PasswordStdin bool
	AuthFile      string
}

func runBuildLogin(cmd *cobra.Command, opts loginOptions) error {
	if opts.PasswordStdin && opts.Password != "" {
		return errors.New("--password and --password-stdin cannot be used together")
	}

	server := normalizeRegistryServer(opts.Server)
	username := strings.TrimSpace(opts.Username)
	password := opts.Password

	var err error
	if username == "" {
		username, err = promptForInput(cmd, "Username")
		if err != nil {
			return err
		}
	}
	if username == "" {
		return errors.New("username is required")
	}

	if opts.PasswordStdin {
		password, err = readPasswordFromStdin(cmd)
		if err != nil {
			return err
		}
	} else if password == "" {
		password, err = promptForPassword(cmd, "Password")
		if err != nil {
			return err
		}
	}

	if password == "" {
		return errors.New("password/token is required")
	}

	if err := pingRegistryFn(server, username, password); err != nil {
		return fmt.Errorf("registry authentication failed: %w", err)
	}

	cfg, err := dockerconfig.LoadConfigFile(opts.AuthFile, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	auth := types.AuthConfig{ServerAddress: server, Username: username, Password: password}
	store := cfg.GetCredentialsStore(server)
	if err := dockerconfig.EnsureConfigDir(cfg.Filename); err != nil {
		return err
	}

	if err := store.Store(auth); err != nil {
		return fmt.Errorf("store credentials: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save docker config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Login Succeeded (%s)\n", server)
	return nil
}

func runBuildLogout(cmd *cobra.Command, server string, authFile string) error {
	normalized := normalizeRegistryServer(server)
	cfg, err := dockerconfig.LoadConfigFile(authFile, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	store := cfg.GetCredentialsStore(normalized)
	if err := store.Erase(normalized); err != nil {
		if dockercred.IsErrCredentialsNotFound(err) {
			fmt.Fprintf(cmd.OutOrStdout(), "Not logged in to %s\n", normalized)
			return nil
		}
		return fmt.Errorf("erase credentials: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save docker config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Removed credentials for %s\n", normalized)
	return nil
}

func promptForInput(cmd *cobra.Command, label string) (string, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", label)
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	type result struct {
		value string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(cmd.InOrStdin())
		value, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			ch <- result{err: err}
			return
		}
		ch <- result{value: strings.TrimSpace(value)}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		return res.value, res.err
	}
}

func promptForPassword(cmd *cobra.Command, label string) (string, error) {
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	if file, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", label)
		type result struct {
			bytes []byte
			err   error
		}
		ch := make(chan result, 1)
		go func() {
			bytes, err := term.ReadPassword(int(file.Fd()))
			ch <- result{bytes: bytes, err: err}
		}()
		select {
		case <-ctx.Done():
			// Best-effort: ensure we end the prompt line.
			fmt.Fprintln(cmd.ErrOrStderr())
			return "", ctx.Err()
		case res := <-ch:
			fmt.Fprintln(cmd.ErrOrStderr())
			if res.err != nil {
				return "", res.err
			}
			return strings.TrimSpace(string(res.bytes)), nil
		}
	}
	return promptForInput(cmd, label)
}

func readPasswordFromStdin(cmd *cobra.Command) (string, error) {
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(cmd.InOrStdin())
		ch <- result{data: data, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		return strings.TrimRight(strings.TrimRight(string(res.data), "\n"), "\r"), nil
	}
}

func pingRegistry(server, username, password string) error {
	endpoint := registryPingEndpoint(server)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := doRegistryPing(client, endpoint, username, password)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("unauthorized (check username/token)")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func doRegistryPing(client *http.Client, endpoint, username, password string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(endpoint, "/")+"/v2/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	chal := resp.Header.Get("WWW-Authenticate")
	_ = resp.Body.Close()

	scheme, params := parseWWWAuthenticate(chal)
	switch strings.ToLower(scheme) {
	case "basic":
		req2, err := http.NewRequest(http.MethodGet, strings.TrimRight(endpoint, "/")+"/v2/", nil)
		if err != nil {
			return nil, err
		}
		if username != "" || password != "" {
			req2.SetBasicAuth(username, password)
		}
		return client.Do(req2)
	case "bearer":
		realm := strings.TrimSpace(params["realm"])
		if realm == "" {
			req2, err := http.NewRequest(http.MethodGet, strings.TrimRight(endpoint, "/")+"/v2/", nil)
			if err != nil {
				return nil, err
			}
			if username != "" || password != "" {
				req2.SetBasicAuth(username, password)
			}
			return client.Do(req2)
		}
		token, err := fetchBearerToken(client, realm, params["service"], params["scope"], username, password)
		if err != nil {
			return nil, err
		}
		req3, err := http.NewRequest(http.MethodGet, strings.TrimRight(endpoint, "/")+"/v2/", nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req3.Header.Set("Authorization", "Bearer "+token)
		}
		return client.Do(req3)
	default:
		req2, err := http.NewRequest(http.MethodGet, strings.TrimRight(endpoint, "/")+"/v2/", nil)
		if err != nil {
			return nil, err
		}
		if username != "" || password != "" {
			req2.SetBasicAuth(username, password)
		}
		return client.Do(req2)
	}
}

func fetchBearerToken(client *http.Client, realm, service, scope, username, password string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(realm))
	if err != nil {
		return "", err
	}
	q := u.Query()
	if strings.TrimSpace(service) != "" {
		q.Set("service", strings.TrimSpace(service))
	}
	if strings.TrimSpace(scope) != "" {
		q.Set("scope", strings.TrimSpace(scope))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("token exchange failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	// best-effort JSON decode; if it fails, treat it as auth failure.
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("token exchange failed: %w", err)
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	return payload.AccessToken, nil
}

func parseWWWAuthenticate(header string) (scheme string, params map[string]string) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", map[string]string{}
	}
	parts := strings.SplitN(header, " ", 2)
	scheme = strings.TrimSpace(parts[0])
	params = map[string]string{}
	if len(parts) < 2 {
		return scheme, params
	}
	rest := strings.TrimSpace(parts[1])
	for _, part := range splitCommaKV(rest) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if k == "" {
			continue
		}
		params[k] = v
	}
	return scheme, params
}

func splitCommaKV(s string) []string {
	out := []string{}
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case ',':
			if inQuote {
				cur.WriteRune(r)
				continue
			}
			part := strings.TrimSpace(cur.String())
			cur.Reset()
			if part != "" {
				out = append(out, part)
			}
		default:
			cur.WriteRune(r)
		}
	}
	if tail := strings.TrimSpace(cur.String()); tail != "" {
		out = append(out, tail)
	}
	return out
}

func registryPingEndpoint(server string) string {
	normalized := strings.TrimSpace(server)
	switch normalized {
	case "", "docker.io", "index.docker.io", "registry-1.docker.io", defaultRegistryServer:
		return "https://registry-1.docker.io"
	}
	if strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://") {
		return strings.TrimRight(normalized, "/")
	}
	return "https://" + strings.TrimRight(normalized, "/")
}

func normalizeRegistryServer(server string) string {
	trimmed := strings.TrimSpace(server)
	if trimmed == "" || strings.EqualFold(trimmed, "docker.io") || strings.EqualFold(trimmed, "index.docker.io") || strings.EqualFold(trimmed, "registry-1.docker.io") || trimmed == defaultRegistryServer {
		return defaultRegistryServer
	}
	return trimmed
}
