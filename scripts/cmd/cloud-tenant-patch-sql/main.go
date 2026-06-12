// Prints tenantschema.CloudTenantPatchSQL for shell scripts.
package main

import (
	"fmt"

	"encore.app/wabantu/shared/tenantschema"
)

func main() {
	fmt.Print(tenantschema.CloudTenantPatchSQL)
}
