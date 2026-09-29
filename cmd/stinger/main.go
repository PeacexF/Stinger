// Command stinger is a high-performance SMTP email verifier.
package main

import (
	"os"

	"github.com/PeacexF/Stinger/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
