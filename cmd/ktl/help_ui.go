package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/example/ktl/internal/helpui"
	"github.com/example/ktl/internal/logging"
	"github.com/spf13/cobra"
)

func newHelpCommand(root *cobra.Command) *cobra.Command {
	var uiAddr string
	var showAll bool
	cmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(uiAddr) != "" {
				logLevel, _ := cmd.Root().PersistentFlags().GetString("log-level")
				if strings.TrimSpace(logLevel) == "" {
					logLevel = "info"
				}
				logger, err := logging.New(logLevel)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Serving help UI at %s\n", formatHelpURL(uiAddr))
				var opts []helpui.Option
				if showAll {
					opts = append(opts, helpui.WithAll())
				}
				return helpui.New(uiAddr, root, logger.WithName("help-ui"), opts...).Run(cmd.Context())
			}
			target, _, err := cmd.Root().Find(args)
			if err != nil || target == nil {
				return cmd.Root().Help()
			}
			target.SetContext(cmd.Context())
			return target.Help()
		},
	}
	cmd.Flags().StringVar(&uiAddr, "ui", "", "Serve the interactive help UI at this address (e.g. :8080)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8080"
	}
	cmd.Flags().BoolVar(&showAll, "all", false, "Include hidden/internal flags and env vars")
	decorateCommandHelp(cmd, "Help Flags")
	return cmd
}

func formatHelpURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		if strings.TrimSpace(h) == "" || h == "0.0.0.0" || h == "::" {
			host = "127.0.0.1:" + p
		}
	}
	u := url.URL{Scheme: "http", Host: host, Path: "/"}
	return u.String()
}
