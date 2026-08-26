package buyerflow

// regressionCase — skenario regresi dari percakapan WA nyata (Omah Apparel thread Jul 2026).
type regressionCase struct {
	name        string
	input       string
	priorInputs []string
	wantPath    string
	wantSubstr  []string
	wantNot     []string
	extraCheck  func(t interface {
		Helper()
		Fatal(...any)
	}, out TurnOutcome)
}

var regressionCases = []regressionCase{
	{
		name:     "rekening_from_kb_not_catalog",
		input:    "bisa minta nomor rekeningnya ga sih ?",
		wantPath: PathPaymentFAQ,
		wantSubstr: []string{"BCA", "110220330"},
		wantNot:  []string{"belum menemukan data tersebut di katalog"},
	},
	{
		name:     "cara_bayar_from_kb",
		input:    "saya bayarnya gimana ini ?",
		wantPath: PathPaymentFAQ,
		wantSubstr: []string{"BCA"},
		wantNot:  []string{"tunggu konfirmasi CS"},
	},
	{
		name:     "bare_order_ref_status",
		input:    "WB-58D662BC",
		wantPath: PathOrderStatus,
		wantNot:  []string{"di luar topik bisnis"},
	},
	{
		name:     "detail_pesanan_with_ref",
		input:    "mau lihat detail pesanan WB-58D662BC",
		wantPath: PathOrderStatus,
	},
	{
		name:     "status_pesanan_with_ref",
		input:    "gimana status nya pesanan WB-372AF9ED",
		wantPath: PathOrderStatus,
	},
	{
		name:     "pending_orders_list",
		input:    "saya masih ada pesanan pending ga ya ?",
		wantPath: PathOrderStatus,
	},
	{
		name:     "best_seller_catalog_not_payment",
		input:    "best seller di toko ini apa ?",
		wantPath: PathCatalogDB,
		wantSubstr: []string{"Abon"},
	},
}
