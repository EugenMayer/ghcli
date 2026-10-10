// Package browser opens URLs in the user's web browser.
package browser

import (
	"os/exec"
	"runtime"
)

// Open opens url in the default browser. It fails without a desktop, e.g. over SSH.
func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
