// Package startup installs the tlsguard on http.DefaultTransport and runs the
// eMudhra self-check (log only, asynchronous, never panics).
//
// Every Encore service blank-imports this package (file tlsguard.go in each
// service dir) so the hook exists in every process, however services are
// grouped at deploy time. Package init runs once per process.
package startup

import (
	"encore.app/wabantu/shared/tlsguard"
)

func init() {
	if tlsguard.CurrentMode() != tlsguard.ModeOff {
		tlsguard.InstallDefaultTransport()
	}
	go tlsguard.RunSelfCheck()
}
