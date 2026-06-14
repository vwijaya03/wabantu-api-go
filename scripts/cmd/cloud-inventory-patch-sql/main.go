// Prints tenantschema.InventorySchemaSQL for the cloud inventory apply script.
package main

import (
	"fmt"

	"encore.app/wabantu/shared/tenantschema"
)

func main() {
	fmt.Print(tenantschema.InventorySchemaSQL)
}
