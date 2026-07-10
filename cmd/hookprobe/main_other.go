//go:build !windows

// hookprobe is Windows-only for now — it uses a raw WH_MOUSE_LL hook to prove
// mouse side buttons reach a global hook. This stub keeps the package building
// on other platforms until the cross-platform (robotgo) path is wired in.
package main

import "fmt"

func main() {
	fmt.Println("hookprobe is Windows-only (raw WH_MOUSE_LL hook). Nothing to do on this OS.")
}
