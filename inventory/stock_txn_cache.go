package inventory

import "sync"

var stockTxnBackfillDoneCache sync.Map // tenant schema name -> bool

func markStockTxnBackfillDoneCached(schema string) {
	stockTxnBackfillDoneCache.Store(schema, true)
}

func isStockTxnBackfillDoneCached(schema string) bool {
	v, ok := stockTxnBackfillDoneCache.Load(schema)
	if !ok {
		return false
	}
	done, ok := v.(bool)
	return ok && done
}
