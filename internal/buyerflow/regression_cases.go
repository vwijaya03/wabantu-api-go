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
	// --- Acceptance criteria (RAG smooth conversations plan) ---
	{
		name:     "greeting_sore_bang",
		input:    "sore bang",
		wantPath: PathGreeting,
	},
	{
		name:     "catalog_list_toko_jual_apa",
		input:    "toko ini jual apa aja?",
		wantPath: PathCatalogDB,
		wantSubstr: []string{"Pria", "Anak", "Abon"},
	},
	{
		name:     "catalog_excluding_abon",
		input:    "selain abon sapi ada apa aja?",
		wantPath: PathCatalogDB,
		wantSubstr: []string{"boxer", "BOXER"},
	},
	{
		name:     "boxer_mono_L_price",
		input:    "boxer mono spot ukuran L berapa?",
		wantPath: PathCatalogDB,
		wantSubstr: []string{"56900", "L"},
		wantNot:  []string{"- M"},
	},
	{
		name:     "boxer_mono_sizes_list",
		input:    "boxer mono spot ada ukuran apa aja?",
		wantPath: PathCatalogDB,
		wantSubstr: []string{"L", "M"},
	},
	{
		name:     "shipping_eta_faq_direct",
		input:    "berapa lama pengiriman?",
		wantPath: PathShippingFAQ,
		wantSubstr: []string{"2-3 hari"},
	},
	{
		name:     "shipping_luar_kota_faq",
		input:    "bisa kirim ke luar kota?",
		wantPath: PathShippingFAQ,
		wantSubstr: []string{"luar kota"},
	},
	{
		name:     "shipping_quote_template",
		input:    "berapa ongkir ke surabaya?",
		wantPath: PathShippingFAQ,
		wantSubstr: []string{"alamat lengkap"},
	},
	{
		name:     "order_abon_two_pcs",
		input:    "mau beli abon sapi 2 pcs",
		wantPath: PathOrderFlow,
	},
	{
		name:     "greeting_good_evening",
		input:    "good evening",
		wantPath: PathGreeting,
	},
	{
		name:     "catalog_exclusion_list_lainnya",
		input:    "selain abon sapi ada list lainnya?",
		wantPath: PathCatalogDB,
		wantNot:  []string{"belum menemukan data tersebut di katalog"},
	},
	{
		name:     "cart_complaint_not_order_status",
		input:    "pesanan saya ada 2 loh ya",
		priorInputs: []string{
			"mau beli abon sapi 2 pcs",
			"cadbury mini 1 pcs",
		},
		wantPath: PathOrderFlow,
		wantSubstr: []string{"ringkasan"},
		wantNot:  []string{"belum ada pesanan"},
	},
	{
		name:     "add_more_policy_not_catalog",
		input:    "masih mau order item yang lain?",
		priorInputs: []string{"mau beli abon sapi 2 pcs"},
		wantPath: PathConsulting,
		wantNot:  []string{"Pria Dewasa", "Anak Perempuan"},
	},
	{
		name:     "off_topic_not_faq_direct",
		input:    "resep nasi goreng enak gimana?",
		wantPath: PathConsulting,
		extraCheck: func(t interface {
			Helper()
			Fatal(...any)
		}, out TurnOutcome) {
			if out.Path == PathFAQDirect {
				t.Fatal("off-topic must not use faq_direct")
			}
		},
	},
	{
		name:     "catalog_list_not_faq_direct",
		input:    "minta list produk",
		wantPath: PathCatalogDB,
		extraCheck: func(t interface {
			Helper()
			Fatal(...any)
		}, out TurnOutcome) {
			if out.Path == PathFAQDirect {
				t.Fatal("catalog list must not use faq_direct")
			}
		},
	},
}
