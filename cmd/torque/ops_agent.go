package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/ops/inventory"
	"github.com/spf13/cobra"
)

var collectOpsAgentStatus = heartbeat.Collect
var snapshotOpsAgentStore = heartbeat.SnapshotFromStore
var ingestOpsAgentRegistry = heartbeat.IngestJetStream

func newOpsAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Inspect and manage ops agents",
	}
	cmd.AddCommand(newOpsAgentStatusCommand())
	cmd.AddCommand(newOpsAgentRegistryCommand())
	decorateCommandHelp(cmd, "Ops Agent Flags")
	return cmd
}

func newOpsAgentStatusCommand() *cobra.Command {
	var source string
	var natsURL string
	var creds string
	var nkey string
	var tenant string
	var selectorValues []string
	var timeout time.Duration
	var staleAfter time.Duration
	storeFlags := opsAgentStoreFlags{}
	format := "table"

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Collect live ops-agent heartbeats from NATS",
		Example: `  torque ops agent status --nats-url nats://127.0.0.1:4222 --tenant lab
  torque ops agent status --selector role=mysql --format json
  torque-agent nats heartbeat --agent-id host-141 --tenant lab --label role=mysql`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops agent status does not accept positional arguments")
			}
			selector, err := inventory.ParseSelector(selectorValues)
			if err != nil {
				return err
			}
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than zero")
			}
			if staleAfter <= 0 {
				return fmt.Errorf("--stale-after must be greater than zero")
			}
			snapshot, err := collectOpsAgentStatusSnapshot(cmd.Context(), opsAgentStatusRequest{
				source:     source,
				natsURL:    natsURL,
				creds:      creds,
				nkey:       nkey,
				tenant:     tenant,
				selector:   selector,
				timeout:    timeout,
				staleAfter: staleAfter,
				store:      storeFlags,
			})
			if err != nil {
				return err
			}
			switch format {
			case "json":
				raw, err := json.MarshalIndent(snapshot, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return err
			case "table", "":
				return renderOpsAgentStatusTable(cmd.OutOrStdout(), snapshot)
			default:
				return fmt.Errorf("--format must be table or json")
			}
		},
	}
	cmd.Flags().StringVar(&source, "source", "live", "Status source: live or store")
	cmd.Flags().StringVar(&natsURL, "nats-url", firstNonEmpty(os.Getenv("TORQUE_NATS_URL"), os.Getenv("TORQUE_NATS_SERVER")), "NATS server URL")
	cmd.Flags().StringVar(&creds, "creds", strings.TrimSpace(os.Getenv("TORQUE_NATS_CREDS")), "NATS user credentials file")
	cmd.Flags().StringVar(&nkey, "nkey", strings.TrimSpace(os.Getenv("TORQUE_NATS_NKEY")), "NATS NKey seed file")
	cmd.Flags().StringVar(&tenant, "tenant", firstNonEmpty(os.Getenv("TORQUE_AGENT_TENANT"), heartbeat.DefaultTenant), "Tenant namespace to inspect")
	cmd.Flags().StringArrayVar(&selectorValues, "selector", nil, "Agent label selector as key=value (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Second, "Heartbeat collection window and NATS connection timeout")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 45*time.Second, "Mark agents stale after this age")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	addOpsAgentStoreFlags(cmd, &storeFlags)
	_ = cmd.RegisterFlagCompletionFunc("source", cobra.FixedCompletions([]string{"live", "store"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Ops Agent Status Flags")
	return cmd
}

func newOpsAgentRegistryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Compact durable agent events into the ops-agent registry",
	}
	cmd.AddCommand(newOpsAgentRegistryCompactCommand())
	decorateCommandHelp(cmd, "Ops Agent Registry Flags")
	return cmd
}

func newOpsAgentRegistryCompactCommand() *cobra.Command {
	var natsURL string
	var creds string
	var nkey string
	var tenant string
	var timeout time.Duration
	var wait time.Duration
	var staleAfter time.Duration
	var stream string
	var durable string
	var batch int
	var maxMessages int
	storeFlags := opsAgentStoreFlags{}
	format := "json"

	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Consume JetStream heartbeats and write compact registry status",
		Example: `  torque ops agent registry compact --nats-url nats://127.0.0.1:4222 --tenant lab --store etcd --etcd-endpoints http://127.0.0.1:2379
  torque ops agent registry compact --max-messages 100 --store file --store-path ./.torque/ops/agent-registry.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops agent registry compact does not accept positional arguments")
			}
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than zero")
			}
			if wait <= 0 {
				return fmt.Errorf("--wait must be greater than zero")
			}
			if staleAfter <= 0 {
				return fmt.Errorf("--stale-after must be greater than zero")
			}
			store, err := openOpsAgentStore(cmd.Context(), storeFlags)
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			result, err := ingestOpsAgentRegistry(ctx, heartbeat.IngestOptions{
				NATS: heartbeat.NATSConfig{
					Server:       strings.TrimSpace(natsURL),
					Creds:        strings.TrimSpace(creds),
					NKey:         strings.TrimSpace(nkey),
					Timeout:      timeout,
					Name:         "torque-ops-agent-registry",
					JetStream:    true,
					Stream:       strings.TrimSpace(stream),
					StreamMaxAge: 24 * time.Hour,
				},
				Store:       store,
				Tenant:      tenant,
				Stream:      stream,
				Durable:     durable,
				Batch:       batch,
				MaxMessages: maxMessages,
				Wait:        wait,
				StaleAfter:  staleAfter,
			})
			if err != nil {
				return err
			}
			switch format {
			case "json":
				raw, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return err
			case "table", "":
				return renderOpsAgentIngestTable(cmd.OutOrStdout(), result)
			default:
				return fmt.Errorf("--format must be table or json")
			}
		},
	}
	cmd.Flags().StringVar(&natsURL, "nats-url", firstNonEmpty(os.Getenv("TORQUE_NATS_URL"), os.Getenv("TORQUE_NATS_SERVER")), "NATS server URL")
	cmd.Flags().StringVar(&creds, "creds", strings.TrimSpace(os.Getenv("TORQUE_NATS_CREDS")), "NATS user credentials file")
	cmd.Flags().StringVar(&nkey, "nkey", strings.TrimSpace(os.Getenv("TORQUE_NATS_NKEY")), "NATS NKey seed file")
	cmd.Flags().StringVar(&tenant, "tenant", firstNonEmpty(os.Getenv("TORQUE_AGENT_TENANT"), heartbeat.DefaultTenant), "Tenant namespace to compact")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "Maximum compaction runtime and NATS connection timeout")
	cmd.Flags().DurationVar(&wait, "wait", time.Second, "Per-fetch wait when the durable consumer has no messages")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 45*time.Second, "Mark agents stale after this age")
	cmd.Flags().StringVar(&stream, "stream", heartbeat.DefaultEventStream, "JetStream stream for durable agent events")
	cmd.Flags().StringVar(&durable, "durable", heartbeat.DefaultRegistryDurable, "Durable consumer name")
	cmd.Flags().IntVar(&batch, "batch", 64, "JetStream pull batch size")
	cmd.Flags().IntVar(&maxMessages, "max-messages", 0, "Stop after compacting this many messages (0 means until timeout/no messages)")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	addOpsAgentStoreFlags(cmd, &storeFlags)
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Ops Agent Registry Compact Flags")
	return cmd
}

func renderOpsAgentStatusTable(out io.Writer, snapshot heartbeat.Snapshot) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AGENT\tTARGET\tHEALTH\tSTATE\tAGE\tVERSION\tLABELS\tCAPABILITIES")
	for _, agent := range snapshot.Agents {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			emptyDash(agent.AgentID),
			emptyDash(agent.TargetID),
			emptyDash(agent.Health),
			emptyDash(agent.State),
			emptyDash(agent.Age),
			emptyDash(agent.Version),
			inventory.FormatLabels(agent.Labels),
			inventory.FormatList(agent.Capabilities),
		)
	}
	fmt.Fprintf(tw, "\nTOTAL\t%d\tREADY\t%d\tSTALE\t%d\tDEGRADED\t%d\n", snapshot.Summary.Total, snapshot.Summary.Ready, snapshot.Summary.Stale, snapshot.Summary.Degraded)
	return tw.Flush()
}

func renderOpsAgentIngestTable(out io.Writer, result heartbeat.IngestResult) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TENANT\tSTREAM\tCONSUMER\tPROCESSED\tSTORED\tLAST_SEQUENCE\tSTATUS")
	fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%s\n", result.Tenant, result.Stream, result.Consumer, result.Processed, result.Stored, result.LastSequence, result.Status)
	return tw.Flush()
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

type opsAgentStoreFlags struct {
	store         string
	storePath     string
	etcdEndpoints []string
	etcdPrefix    string
	storeTimeout  time.Duration
}

type opsAgentStatusRequest struct {
	source     string
	natsURL    string
	creds      string
	nkey       string
	tenant     string
	selector   map[string]string
	timeout    time.Duration
	staleAfter time.Duration
	store      opsAgentStoreFlags
}

func collectOpsAgentStatusSnapshot(ctx context.Context, req opsAgentStatusRequest) (heartbeat.Snapshot, error) {
	switch strings.ToLower(strings.TrimSpace(req.source)) {
	case "", "live":
		return collectOpsAgentStatus(ctx, heartbeat.CollectOptions{
			NATS: heartbeat.NATSConfig{
				Server:  strings.TrimSpace(req.natsURL),
				Creds:   strings.TrimSpace(req.creds),
				NKey:    strings.TrimSpace(req.nkey),
				Timeout: req.timeout,
				Name:    "torque-ops-agent-status",
			},
			Tenant:     req.tenant,
			Selector:   req.selector,
			StaleAfter: req.staleAfter,
			Listen:     req.timeout,
		})
	case "store":
		store, err := openOpsAgentStore(ctx, req.store)
		if err != nil {
			return heartbeat.Snapshot{}, err
		}
		defer func() { _ = store.Close() }()
		return snapshotOpsAgentStore(ctx, store, heartbeat.SnapshotRequest{
			Tenant:     req.tenant,
			Selector:   req.selector,
			Now:        time.Now(),
			StaleAfter: req.staleAfter,
		})
	default:
		return heartbeat.Snapshot{}, fmt.Errorf("--source must be live or store")
	}
}

func addOpsAgentStoreFlags(cmd *cobra.Command, flags *opsAgentStoreFlags) {
	flags.store = firstNonEmpty(os.Getenv("TORQUE_AGENT_REGISTRY_STORE"), "file")
	flags.storePath = firstNonEmpty(os.Getenv("TORQUE_AGENT_REGISTRY_STORE_PATH"), filepathDefaultAgentRegistry())
	flags.etcdEndpoints = heartbeat.ParseEtcdEndpoints(os.Getenv("TORQUE_ETCD_ENDPOINTS"))
	flags.etcdPrefix = firstNonEmpty(os.Getenv("TORQUE_ETCD_PREFIX"), heartbeat.DefaultStorePrefix)
	flags.storeTimeout = 5 * time.Second
	cmd.Flags().StringVar(&flags.store, "store", flags.store, "Registry store: file or etcd")
	cmd.Flags().StringVar(&flags.storePath, "store-path", flags.storePath, "File registry store path")
	cmd.Flags().StringSliceVar(&flags.etcdEndpoints, "etcd-endpoints", flags.etcdEndpoints, "etcd endpoints for registry store")
	cmd.Flags().StringVar(&flags.etcdPrefix, "etcd-prefix", flags.etcdPrefix, "etcd key prefix for registry store")
	cmd.Flags().DurationVar(&flags.storeTimeout, "store-timeout", flags.storeTimeout, "Registry store dial/request timeout")
	_ = cmd.RegisterFlagCompletionFunc("store", cobra.FixedCompletions([]string{"file", "etcd"}, cobra.ShellCompDirectiveNoFileComp))
}

func openOpsAgentStore(ctx context.Context, flags opsAgentStoreFlags) (heartbeat.RegistryStore, error) {
	switch strings.ToLower(strings.TrimSpace(flags.store)) {
	case "", "file":
		return heartbeat.NewFileStore(flags.storePath)
	case "etcd":
		storeCtx, cancel := context.WithTimeout(ctx, flags.storeTimeout)
		defer cancel()
		return heartbeat.NewEtcdStore(storeCtx, heartbeat.EtcdConfig{
			Endpoints:   flags.etcdEndpoints,
			Prefix:      flags.etcdPrefix,
			DialTimeout: flags.storeTimeout,
		})
	default:
		return nil, fmt.Errorf("--store must be file or etcd")
	}
}

func filepathDefaultAgentRegistry() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".torque", "ops", "agent-registry.json")
	}
	return filepath.Join(os.TempDir(), "torque-agent-registry.json")
}
