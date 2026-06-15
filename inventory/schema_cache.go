package inventory

import "sync"

// inventorySchemaGen — bump when InventoryModuleReady table/column checklist changes
// so per-process cache invalidates after deploy with new migrations.
const inventorySchemaGen = 1

var inventorySchemaReady sync.Map // schema name -> inventorySchemaGen

func markInventorySchemaReady(schemaName string) {
	inventorySchemaReady.Store(schemaName, inventorySchemaGen)
}

func isInventorySchemaReadyCached(schemaName string) bool {
	v, ok := inventorySchemaReady.Load(schemaName)
	if !ok {
		return false
	}
	gen, ok := v.(int)
	return ok && gen == inventorySchemaGen
}
