// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/fabrizio/md2confl/cli"
)

// Version is set at build time via ldflags.
var Version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], Version, os.Stdout, os.Stderr))
}
