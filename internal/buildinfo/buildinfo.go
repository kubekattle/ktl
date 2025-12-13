// buildinfo.go captures build metadata (version, commit, date) for use in --version outputs.
package buildinfo

// Version is injected at build time via -ldflags and defaults to dev.
var Version = "dev"
