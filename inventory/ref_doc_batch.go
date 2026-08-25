package inventory

import (
	appdb "encore.app/wabantu/shared/db"
	"context"
	"fmt"
)

type refDocKey struct {
	refType string
	refID   string
}

func batchResolveRefDocNos(ctx context.Context, sch appdb.SchemaSQL, q querier, refs []refDocKey) map[refDocKey]string {
	out := make(map[refDocKey]string, len(refs))
	if len(refs) == 0 {
		return out
	}

	billIDs := make([]string, 0)
	salesReturnIDs := make([]string, 0)
	stockTxnIDs := make([]string, 0)
	pending := make([]refDocKey, 0, len(refs))

	for _, r := range refs {
		if r.refID == "" {
			continue
		}
		switch r.refType {
		case "order":
			out[r] = formatOrderRef(r.refID)
		case "bill":
			billIDs = append(billIDs, r.refID)
			pending = append(pending, r)
		case "sales_return":
			salesReturnIDs = append(salesReturnIDs, r.refID)
			pending = append(pending, r)
		case TxnKindAdjustment, TxnKindTransfer, TxnKindOpeningBalance, TxnKindRevaluation:
			stockTxnIDs = append(stockTxnIDs, r.refID)
			pending = append(pending, r)
		}
	}

	byID := make(map[string]string)
	loadRefDocIDs(ctx, sch, q, billIDs, `
		SELECT id::text, bill_no FROM pur_bill WHERE id IN (%s)`, byID)
	loadRefDocIDs(ctx, sch, q, salesReturnIDs, `
		SELECT id::text, return_no FROM inv_sales_return WHERE id IN (%s)`, byID)
	loadRefDocIDs(ctx, sch, q, stockTxnIDs, `
		SELECT id::text, doc_no FROM inv_stock_transaction WHERE id IN (%s)`, byID)

	for _, r := range pending {
		if docNo := byID[r.refID]; docNo != "" {
			out[r] = docNo
		}
	}
	return out
}

func loadRefDocIDs(ctx context.Context, sch appdb.SchemaSQL, q querier, ids []string, queryFmt string, out map[string]string) {
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return
	}
	clause, args := inClause(1, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := qquery(ctx, sch, q, fmt.Sprintf(queryFmt, clause), args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, docNo string
		if err := rows.Scan(&id, &docNo); err != nil {
			return
		}
		out[id] = docNo
	}
}

func resolveRefDocNoFromMap(m map[refDocKey]string, refType, refID string) string {
	if refID == "" {
		return ""
	}
	if refType == "order" {
		return formatOrderRef(refID)
	}
	if docNo := m[refDocKey{refType: refType, refID: refID}]; docNo != "" {
		return docNo
	}
	return ""
}
