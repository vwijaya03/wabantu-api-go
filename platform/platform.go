// Package platform wires cross-cutting production init (observability).
package platform

import (
	"encore.app/wabantu/shared/sentry"
)

func init() {
	sentry.Init()
}
