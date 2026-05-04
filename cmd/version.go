package cmd

// Build-time variables injected by the linker via -X flags in the Makefile.
var (
	version   string
	commit    string
	buildTime string
)
