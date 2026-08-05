package cli

import (
	"fmt"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/spf13/cobra"
)

func parentCommand(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		}
		return cmd.Help()
	}}
}

func (a *app) serverCmd() *cobra.Command {
	cmd := parentCommand("server", "manage servers")
	cmd.AddCommand(
		a.serverCreateCmd(),
		a.serverBootstrapCmd(),
		a.serverListCmd(),
		a.serverStatusCmd(),
		a.serverDoctorCmd(),
		a.serverSSHCmd(),
		a.serverDiscoverCmd(),
		a.serverImportCmd(),
		a.serverDeleteCmd(),
		a.serverStartCmd(),
		a.serverStopCmd(),
		a.serverRestartCmd(),
	)
	return cmd
}

func (a *app) serverCreateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create NAME", Short: "create a server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runCreateCommand(cmd)
	}}
	cmd.Flags().StringVar(&a.create.ComputeName, "compute-name", "", "compute provider server name")
	cmd.Flags().StringVar(&a.create.Location, "location", "", "compute location")
	cmd.Flags().StringVar(&a.create.Size, "size", "", "compute server size")
	cmd.Flags().StringVar(&a.create.Image, "image", "", "compute image")
	cmd.Flags().StringVar(&a.create.AdminUser, "admin-user", "", "remote admin username")
	cmd.Flags().StringVar(&a.create.TailscaleTailnet, "tailscale-tailnet", "", "Tailscale tailnet")
	cmd.Flags().StringVar(&a.create.TailscaleTags, "tailscale-tags", "", "comma-separated Tailscale tags")
	cmd.Flags().StringVar(&a.create.Ingress, "ingress", "", "public ingress mode: none or cloudflare-tunnel")
	cmd.Flags().StringVar(&a.create.CloudflareAccountID, "cloudflare-account-id", "", "Cloudflare account ID")
	cmd.Flags().StringVar(&a.create.CloudflareTunnelName, "cloudflare-tunnel-name", "", "Cloudflare tunnel name")
	cmd.Flags().StringVar(&a.create.EgressMode, "egress-mode", "", "egress mode: restricted or open")
	return cmd
}

func (a *app) serverListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "list servers", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.runServerList(cmd.Context())
	}}
}

func (a *app) serverStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status NAME", Short: "show server status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerStatus(cmd.Context(), args[0])
	}}
}

func (a *app) serverDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "doctor NAME", Short: "run server checks", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerDoctor(cmd.Context(), args[0])
	}}
	cmd.Flags().BoolVar(&a.doctorFix, "fix", false, "apply failed fixable remote security checks")
	return cmd
}

func (a *app) serverSSHCmd() *cobra.Command {
	return &cobra.Command{Use: "ssh NAME", Short: "open mesh SSH to a server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerSSH(cmd.Context(), args[0])
	}}
}

func (a *app) serverDeleteCmd() *cobra.Command {
	return &cobra.Command{Use: "delete NAME", Short: "delete a server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerDelete(cmd.Context(), args[0])
	}}
}

func (a *app) serverStartCmd() *cobra.Command {
	return &cobra.Command{Use: "start NAME", Short: "power on a server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerPower(cmd.Context(), args[0], compute.PowerStart)
	}}
}

func (a *app) serverStopCmd() *cobra.Command {
	return &cobra.Command{Use: "stop NAME", Short: "gracefully shut down a server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerPower(cmd.Context(), args[0], compute.PowerStop)
	}}
}

func (a *app) serverRestartCmd() *cobra.Command {
	return &cobra.Command{Use: "restart NAME", Short: "restart a server", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a.server = args[0]
		return a.runServerPower(cmd.Context(), args[0], compute.PowerRestart)
	}}
}

func (a *app) globalDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "doctor", Short: "run validation checks", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.runGlobalDoctor()
	}}
	cmd.Flags().BoolVar(&a.doctorFix, "fix", false, "apply failed fixable checks")
	return cmd
}

func (a *app) providerCmd() *cobra.Command {
	cmd := parentCommand("provider", "manage compute providers")
	cmd.AddCommand(a.providerListCmd(), a.providerStatusCmd(), a.providerDoctorCmd())
	return cmd
}

func (a *app) providerListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "list providers", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.runProviderList()
	}}
}

func (a *app) providerStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status NAME", Short: "show provider status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runProviderStatus(cmd.Context(), args[0])
	}}
}

func (a *app) providerDoctorCmd() *cobra.Command {
	return &cobra.Command{Use: "doctor NAME", Short: "run provider checks", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runProviderDoctor(cmd.Context(), args[0])
	}}
}

func (a *app) catalogCmd() *cobra.Command {
	cmd := parentCommand("catalog", "browse provider catalogs")
	cmd.AddCommand(a.catalogLocationsCmd(), a.catalogSizesCmd(), a.catalogImagesCmd())
	return cmd
}

func (a *app) catalogLocationsCmd() *cobra.Command {
	return &cobra.Command{Use: "locations", Short: "list locations", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.runCatalog(cmd.Context(), "locations", "")
	}}
}

func (a *app) catalogSizesCmd() *cobra.Command {
	var location string
	cmd := &cobra.Command{Use: "sizes", Short: "list sizes", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.runCatalog(cmd.Context(), "sizes", location)
	}}
	cmd.Flags().StringVar(&location, "location", "", "filter by location")
	return cmd
}

func (a *app) catalogImagesCmd() *cobra.Command {
	var location string
	cmd := &cobra.Command{Use: "images", Short: "list images", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return a.runCatalog(cmd.Context(), "images", location)
	}}
	cmd.Flags().StringVar(&location, "location", "", "filter by location")
	return cmd
}

func (a *app) ingressCmd() *cobra.Command {
	cmd := parentCommand("ingress", "manage optional public ingress")
	cmd.AddCommand(a.ingressAddCmd(), a.ingressListCmd(), a.ingressRemoveCmd())
	return cmd
}

func (a *app) ingressAddCmd() *cobra.Command {
	var ingressType string
	var hostname string
	cmd := &cobra.Command{Use: "add SERVER", Short: "add ingress", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runIngressAdd(cmd.Context(), args[0], ingressType, hostname)
	}}
	cmd.Flags().StringVar(&ingressType, "type", "", "ingress type")
	cmd.Flags().StringVar(&hostname, "hostname", "", "public hostname")
	return cmd
}

func (a *app) ingressListCmd() *cobra.Command {
	return &cobra.Command{Use: "list SERVER", Short: "list ingress", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runIngressList(cmd.Context(), args[0])
	}}
}

func (a *app) ingressRemoveCmd() *cobra.Command {
	var hostname string
	cmd := &cobra.Command{Use: "remove SERVER", Short: "remove ingress", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return a.runIngressRemove(cmd.Context(), args[0], hostname)
	}}
	cmd.Flags().StringVar(&hostname, "hostname", "", "public hostname")
	return cmd
}
