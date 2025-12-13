// render.go formats ingress/gateway/service readiness for 'ktl diag network'.
package networkstatus

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
)

// RenderOptions toggles inclusion of service table.
type RenderOptions struct {
	ShowServices bool
}

// Print renders network status tables.
func Print(summary *Summary, opts RenderOptions) {
	if summary == nil {
		fmt.Println("No resources found.")
		return
	}
	printIngresses(summary.Ingresses)
	if opts.ShowServices {
		printServices(summary.Services)
	}
	printGateways(summary.Gateways)
}

func printIngresses(ingresses []IngressStatus) {
	if len(ingresses) == 0 {
		fmt.Println("No Ingresses matched the filter.")
		return
	}
	fmt.Println("Ingresses:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "INGRESS\tCLASS\tHOSTS\tLB ADDRESSES\tSERVICES\tTLS")
	for _, ing := range ingresses {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\t%s\n",
			ing.Namespace,
			ing.Name,
			dashIfEmpty(ing.Class),
			strings.Join(ing.Hosts, ","),
			highlightReady(strings.Join(ing.LoadBalancer, ","), ing.Ready),
			strings.Join(ing.ServiceRefs, ","),
			formatTLS(ing.TLS),
		)
	}
	_ = tw.Flush()
}

func printServices(services []ServiceStatus) {
	if len(services) == 0 {
		fmt.Println("\nNo Services matched the filter.")
		return
	}
	fmt.Println("\nServices:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tTYPE\tPORTS\tLB\tEXTERNAL IPs\tREADY/NOT READY")
	for _, svc := range services {
		status := fmt.Sprintf("%d/%d", svc.ReadyEndpoints, svc.NotReady)
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\t%s\n",
			svc.Namespace,
			svc.Name,
			svc.Type,
			strings.Join(svc.Ports, ","),
			strings.Join(svc.LoadBalancerIP, ","),
			strings.Join(svc.ExternalIPs, ","),
			highlightReady(status, svc.NotReady == 0),
		)
	}
	_ = tw.Flush()
}

func printGateways(gateways []GatewayStatus) {
	if len(gateways) == 0 {
		fmt.Println("\nGateways: none detected (cluster may not install Gateway API).")
		return
	}
	fmt.Println("\nGateways:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "GATEWAY\tCLASS\tADDRESSES\tSTATUS\tMESSAGE")
	for _, gw := range gateways {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\n",
			gw.Namespace,
			gw.Name,
			dashIfEmpty(gw.Class),
			strings.Join(gw.Addresses, ","),
			highlightReady("READY", gw.Ready),
			gw.Message,
		)
	}
	_ = tw.Flush()
}

func dashIfEmpty(val string) string {
	if val == "" {
		return "-"
	}
	return val
}

func highlightReady(text string, ready bool) string {
	if color.NoColor {
		return text
	}
	if ready {
		return color.New(color.FgGreen).Sprint(text)
	}
	if text == "" {
		text = "-"
	}
	return color.New(color.FgHiRed).Sprint(text)
}

func formatTLS(entries []TLSSecretStatus) string {
	if len(entries) == 0 {
		return "-"
	}
	var parts []string
	for _, entry := range entries {
		val := entry.Secret
		if val == "" {
			val = "(empty)"
		}
		if !entry.Found && !color.NoColor {
			val = color.New(color.FgHiRed).Sprint(val)
		} else if !entry.Found {
			val = val + " (missing)"
		}
		parts = append(parts, val)
	}
	return strings.Join(parts, ",")
}
