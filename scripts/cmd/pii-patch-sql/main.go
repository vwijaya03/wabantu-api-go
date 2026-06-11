// Prints tenant.PIISchemaPatchSQL for shell scripts (single source of truth).
package main

import (
	"fmt"

	"encore.app/wabantu/shared/tenantschema"
)

func main() {
	fmt.Print(tenantschema.PIISchemaPatchSQL)
}
