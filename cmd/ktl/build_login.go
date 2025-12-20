package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
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
		Args:  cobra.MaximumNArgs(1),
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

	cmd.Flags().StringVarP(&opts.Username, "username", "u", "", "Registry username")
	cmd.Flags().StringVarP(&opts.Password, "password", "p", "", "Registry password or token (prefer --password-stdin)")
	cmd.Flags().BoolVar(&opts.PasswordStdin, "password-stdin", false, "Read the password/token from stdin")

	return cmd
}

func newBuildLogoutCommand(parent *buildCLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout [SERVER]",
		Short: "Log out of a container registry",
		Args:  cobra.MaximumNArgs(1),
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
	reader := bufio.NewReader(cmd.InOrStdin())
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func promptForPassword(cmd *cobra.Command, label string) (string, error) {
	if file, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", label)
		bytes, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(bytes)), nil
	}
	return promptForInput(cmd, label)
}

func readPasswordFromStdin(cmd *cobra.Command) (string, error) {
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return strings.TrimRight(strings.TrimRight(string(data), "\n"), "\r"), nil
}

func pingRegistry(server, username, password string) error {
	endpoint := registryPingEndpoint(server)
	req, err := http.NewRequest(http.MethodGet, endpoint+"/v2/", nil)
	if err != nil {
		return err
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
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
