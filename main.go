// Command tofulock locks and verifies Terraform / OpenTofu *module* sources
// by content digest — closing the gap that the native .terraform.lock.hcl
// leaves open (it records providers only, never modules).
package main

import (
	"os"

	"github.com/Conalh/tofulock/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
