// app_vendor.go contains the 'ktl app vendor' workflow that mirrors external charts/assets into deterministic vendor directories.
package main

import (
	vendircmd "carvel.dev/vendir/pkg/vendir/cmd"
	vendirui "github.com/cppforlife/go-cli-ui/ui"
	"github.com/spf13/cobra"
)

func newAppVendorCommand() *cobra.Command {
	confUI := vendirui.NewConfUI(vendirui.NewNoopLogger())
	cmd := vendircmd.NewDefaultVendirCmd(confUI)
	cmd.Use = "vendor"
	cmd.Short = "Vendor upstream content via embedded vendir"
	cmd.Long = `Expose the full vendir CLI surface inside ktl. Everything available in the standalone
vendir binary—including sync, version, tools sort-semver, and future subcommands—
can be accessed via 'ktl app vendor <subcommand>'.`
	cmd.Example = `  # Show vendir help
  ktl app vendor --help

  # Run a sync using vendir.yml (writes vendir.lock.yml)
  ktl app vendor sync --file vendir.yml --lock-file vendir.lock.yml

  # Use vendir tools sort-semver
  ktl app vendor tools sort-semver v1.2.0 v1.10.3`
	return cmd
}
