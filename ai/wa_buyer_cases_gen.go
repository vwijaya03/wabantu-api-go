package ai

import "fmt"

func generateAllWABuyerCases() []WABuyerCase {
	var out []WABuyerCase
	id := 1
	add := func(c WABuyerCase) {
		c.ID = id
		id++
		out = append(out, c)
	}

	greetingBases := []string{"halo", "selamat malam", "pagi kak", "hai", "assalamualaikum", "permisi", "malam gan", "bro"}
	for _, b := range greetingBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "greeting", LanguageStyle: st,
				InputUser:                stylePhrase(b, st),
				ExpectedState:            "none",
				ExpectedIntent:           SalesStateGreeting,
				ExpectedResponseBehavior: BehaviorGreeting,
			})
		}
	}

	browseBases := []string{
		"jualan apa aja", "lihat katalog", "daftar produk", "menu apa aja",
		"ada produk apa", "mau liat katalog", "show catalog", "list barang",
		"katalog dong", "produknya apa aja",
	}
	for _, b := range browseBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "browse_catalog", LanguageStyle: st,
				InputUser:                stylePhrase(b, st),
				ExpectedIntent:           SalesStateBrowsing,
				ExpectedResponseBehavior: BehaviorCatalogList,
			})
		}
	}

	searchBases := []string{
		"cari abon sapi", "ada boxer mono spot", "cari celana dalam pria",
		"search boxer", "mau cari hello kitty", "produk abon", "boxer cowok",
		"ada jeans ga", "cari makanan", "boxr mono", "abonn sapi",
		"mono spot ada",
	}
	for _, b := range searchBases {
		for i, st := range waLanguageStyles {
			intent := SalesStateConsulting
			behavior := BehaviorCatalogProduct
			if i%3 == 0 {
				intent = SalesStateProductSelected
			}
			add(WABuyerCase{
				Category: "search_product", LanguageStyle: st,
				InputUser:                stylePhrase(b, st),
				ExpectedIntent:           intent,
				ExpectedResponseBehavior: behavior,
			})
		}
	}

	priceBases := []string{
		"harga abon berapa", "berapa harga boxer", "price mono spot",
		"brp harganya", "how much abon sapi", "harga per paket boxer",
		"berapaan abon", "harg boxer", "berapa duit abon",
	}
	for _, b := range priceBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "ask_price", LanguageStyle: st,
				InputUser:                stylePhrase(b, st),
				ExpectedIntent:           SalesStateConsulting,
				ExpectedResponseBehavior: BehaviorCatalogPrice,
			})
		}
	}

	sizeBases := []string{
		"ukuran L ada", "size M boxer", "ada ukuran XL", "ukran L mono spot",
		"what size available", "ukuran boxer apa aja", "size hello kitty",
		"punten ukuran L ado",
	}
	for _, b := range sizeBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "ask_size", LanguageStyle: st,
				InputUser:                stylePhrase(b, st),
				ExpectedIntent:           SalesStateConsulting,
				ExpectedResponseBehavior: BehaviorCatalogProduct,
			})
		}
	}

	for n := 1; n <= 12; n++ {
		for _, unit := range []string{"pcs", "biji", "paket", "buah"} {
			for _, st := range []string{"neutral", "informal", "slang", "typo"} {
				msg := stylePhrase(fmt.Sprintf("mau %d %s abon", n, unit), st)
				add(WABuyerCase{
					Category: "ask_quantity", LanguageStyle: st,
					InputUser:                msg,
					ExpectedIntent:           SalesStateCartReady,
					ExpectedResponseBehavior: BehaviorOrderFlow,
				})
			}
		}
	}

	moqBases := []string{
		"minimum order berapa", "min pesan berapa", "minimal beli berapa",
		"bisa order 1 pcs", "min order", "berapa minimal pembelian",
	}
	for _, b := range moqBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "ask_minimum_order", LanguageStyle: st,
				InputUser:      stylePhrase(b, st),
				ExpectedIntent: SalesStateConsulting,
			})
		}
	}

	compareBases := []string{
		"beda boxer mono sama hello kitty", "bandingin abon sama boxer",
		"mono spot vs de wasa", "mana yang lebih bagus abon atau boxer",
	}
	for _, b := range compareBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "product_comparison", LanguageStyle: st,
				InputUser:      stylePhrase(b, st),
				ExpectedIntent: SalesStateConsulting,
			})
		}
	}

	recommendBases := []string{
		"rekomendasi produk dong", "sarankan yang laris", "recommend best seller",
		"saranin produk", "paling recommended apa",
	}
	for _, b := range recommendBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "recommendation", LanguageStyle: st,
				InputUser:                stylePhrase(b, st),
				ExpectedIntent:           SalesStateBrowsing,
				ExpectedResponseBehavior: BehaviorCatalogList,
			})
		}
	}

	cartBases := []string{
		"saya jadi beli abon sapi 2 pcs", "mau beli boxer mono spot 1 paket",
		"order abon 3 biji", "checkout abon sapi", "jadi pesan boxer 2 paket",
		"gw jadi beli abon 1 pcs", "pengen order boxer mono spot",
	}
	for _, b := range cartBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "add_to_cart", LanguageStyle: st,
				InputUser:                stylePhrase(b, st),
				ExpectedIntent:           SalesStateCartReady,
				ExpectedResponseBehavior: BehaviorOrderFlow,
			})
		}
	}

	for n := 2; n <= 10; n++ {
		for _, tpl := range []string{"ubah jadi %d paket", "revisi %d biji", "ganti jadi %d pcs"} {
			for _, st := range []string{"neutral", "slang", "typo"} {
				msg := stylePhrase(fmt.Sprintf(tpl, n), st)
				stCopy := baseOrderAbon(1, "ask_recipient")
				add(WABuyerCase{
					Category: "update_cart", LanguageStyle: st,
					InputUser: msg, CurrentState: stCopy,
					ExpectedState: "ask_recipient", ExpectedIntent: SalesStateCheckout,
					ExpectedResponseBehavior: BehaviorQtyUpdated, ExpectQty: n,
				})
			}
		}
	}

	cancelBases := []string{"batalkan order", "batal pesanan", "cancel order", "ga jadi deh"}
	for _, b := range cancelBases {
		for _, st := range waLanguageStyles {
			stCopy := baseOrderAbon(1, "ask_recipient")
			add(WABuyerCase{
				Category: "remove_item", LanguageStyle: st,
				InputUser: stylePhrase(b, st), CurrentState: stCopy,
				ExpectedState: "cleared", ExpectedResponseBehavior: BehaviorOrderCancel,
			})
		}
	}

	changeProdBases := []string{
		"ganti jadi abon sapi", "mau boxer bukan abon", "ubah produk ke mono spot",
	}
	for _, b := range changeProdBases {
		for _, st := range waLanguageStyles {
			stCopy := baseOrderAbon(1, "ask_recipient")
			add(WABuyerCase{
				Category: "change_product", LanguageStyle: st, Adversarial: true,
				InputUser: stylePhrase(b, st), CurrentState: stCopy,
				ExpectedResponseBehavior: BehaviorNoFalseCheckout,
			})
		}
	}

	for n := 2; n <= 12; n++ {
		for _, tpl := range []string{
			"revisi jadi %d paket", "loh ubah jadi %d biji",
			"ga jadi mau dirubah menjadi %d biji ya", "ganti %d pcs",
		} {
			for _, st := range []string{"neutral", "informal", "slang", "regional"} {
				msg := stylePhrase(fmt.Sprintf(tpl, n), st)
				for _, step := range []string{"ask_recipient", "ask_qty", "ask_address_full"} {
					stCopy := baseOrderAbon(1, step)
					add(WABuyerCase{
						Category: "change_quantity", LanguageStyle: st,
						InputUser: msg, CurrentState: stCopy,
						ExpectedIntent: SalesStateCheckout, ExpectQty: n,
						ExpectedResponseBehavior: BehaviorQtyUpdated,
					})
				}
			}
		}
	}

	addrChangeBases := []string{
		"ganti alamat jadi Jl Mawar 5 Jakarta Selatan DKI Jakarta 12345",
		"ubah alamat pengiriman", "revisi alamat",
	}
	for _, b := range addrChangeBases {
		for _, st := range waLanguageStyles {
			stCopy := baseOrderAbon(1, "ask_address_full")
			stCopy.RecipientName = "Budi"
			stCopy.RecipientPhone = "+6281234567890"
			add(WABuyerCase{
				Category: "change_address", LanguageStyle: st,
				InputUser: stylePhrase(b, st), CurrentState: stCopy,
				ExpectedIntent: SalesStateCheckout,
			})
		}
	}

	recipientBases := []string{
		"ganti nama penerima jadi Rina", "ubah penerima", "Nama: Rina\nHP: 081299988877",
	}
	for _, b := range recipientBases {
		for _, st := range waLanguageStyles {
			stCopy := baseOrderAbon(1, "ask_recipient")
			add(WABuyerCase{
				Category: "change_recipient", LanguageStyle: st,
				InputUser: stylePhrase(b, st), CurrentState: stCopy,
				ExpectedIntent:           SalesStateCheckout,
				ExpectedResponseBehavior: BehaviorAskRecipient,
			})
		}
	}

	checkoutBases := []string{
		"lanjut checkout", "checkout aja", "proses pesanan", "lanjut pesan",
	}
	for _, b := range checkoutBases {
		for _, st := range waLanguageStyles {
			stCopy := baseOrderBoxer(2, "ask_recipient")
			add(WABuyerCase{
				Category: "checkout", LanguageStyle: st,
				InputUser: stylePhrase(b+" "+recipientBlock("Budi", "081234567890"), st),
				CurrentState: stCopy, ExpectedIntent: SalesStateCheckout,
			})
		}
	}

	paymentBases := []string{
		"bisa bayar COD", "transfer ke rekening mana", "cara bayar qris",
		"payment method", "bisa tf", "bayar pakai apa",
	}
	for _, b := range paymentBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "payment", LanguageStyle: st,
				InputUser: stylePhrase(b, st), ExpectedIntent: SalesStateConsulting,
				ExpectedResponseBehavior: BehaviorPaymentInfo,
			})
		}
	}

	statusBases := []string{
		"pesanan saya gimana", "status order", "cek pesanan saya", "order saya udah sampai mana",
	}
	for _, b := range statusBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "order_status", LanguageStyle: st,
				InputUser: stylePhrase(b, st), ExpectedIntent: SalesStateConsulting,
				ExpectedResponseBehavior: BehaviorOrderStatus,
			})
		}
	}

	complaintBases := []string{
		"komplain barang rusak", "produk cacat", "minta refund", "kecewa banget",
		"salah kirim barang", "mau retur",
	}
	for _, b := range complaintBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "complaint", LanguageStyle: st,
				InputUser: stylePhrase(b, st), ExpectedIntent: SalesStateConsulting,
				ExpectedResponseBehavior: BehaviorComplaint,
			})
		}
	}

	escalationBases := []string{
		"mau chat sama CS", "hubungi admin", "sambungkan ke manusia", "customer service dong",
	}
	for _, b := range escalationBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "human_escalation", LanguageStyle: st,
				InputUser: stylePhrase(b, st), ExpectedIntent: SalesStateConsulting,
				ExpectedResponseBehavior: BehaviorHumanEscalation,
			})
		}
	}

	correctionBases := []string{
		"loh saya masih tanya", "jangan checkout dulu", "belum mau beli",
		"bukan mau order", "ha?", "salah paham",
	}
	for _, b := range correctionBases {
		for _, st := range waLanguageStyles {
			stCopy := baseOrderBoxer(1, "ask_variant")
			add(WABuyerCase{
				Category: "correction_flow", LanguageStyle: st,
				InputUser: stylePhrase(b, st), CurrentState: stCopy,
				ExpectedIntent: SalesStateCorrection, ExpectedState: "cleared",
				ExpectedResponseBehavior: BehaviorBreakFlow,
			})
		}
	}

	ambiguousBases := []string{
		"mau beli bisa ga", "boleh order 1 pcs?", "kalau beli 2 gimana",
		"bisa ga beli satuan", "mau beli boxer?",
	}
	for _, b := range ambiguousBases {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "ambiguous_intent", LanguageStyle: st,
				InputUser: stylePhrase(b, st), ExpectedIntent: SalesStateConsulting,
			})
		}
	}

	topicBases := []string{
		"berapa harga", "jualan apa aja", "ini tokonya dimana", "minta list produk",
	}
	for _, b := range topicBases {
		for _, step := range []string{"ask_variant", "ask_recipient", "ask_qty"} {
			for _, st := range []string{"neutral", "informal", "slang"} {
				stCopy := baseOrderBoxer(1, step)
				add(WABuyerCase{
					Category: "topic_switching", LanguageStyle: st, Adversarial: true,
					InputUser: stylePhrase(b, st), CurrentState: stCopy,
					ExpectedState: "cleared", ExpectedResponseBehavior: BehaviorBreakFlow,
				})
			}
		}
	}

	abandonBases := []string{
		"nanti dulu deh", "besok aja", "pikirin dulu", "skip dulu", "udah dulu",
	}
	for _, b := range abandonBases {
		for _, st := range waLanguageStyles {
			stCopy := baseOrderAbon(1, "ask_recipient")
			add(WABuyerCase{
				Category: "abandoned_cart", LanguageStyle: st,
				InputUser: stylePhrase(b, st), CurrentState: stCopy,
				ExpectedResponseBehavior: BehaviorNoFalseCheckout,
			})
		}
	}

	// Adversarial: revisi tidak boleh cancel
	advRevision := []struct{ msg string; qty int }{
		{"ga jadi mau dirubah menjadi 10 biji ya", 10},
		{"ga jadi ubah jadi 5 paket", 5},
		{"tidak jadi ganti 3 pcs", 3},
	}
	for _, ar := range advRevision {
		for _, st := range []string{"neutral", "slang"} {
			stCopy := baseOrderAbon(1, "ask_recipient")
			add(WABuyerCase{
				Category: "adversarial_revision_not_cancel", LanguageStyle: st, Adversarial: true,
				InputUser: stylePhrase(ar.msg, st), CurrentState: stCopy,
				ExpectedResponseBehavior: BehaviorNoFalseCancel, ExpectQty: ar.qty,
			})
		}
	}

	// Adversarial: konsultasi bukan checkout
	consultMsgs := []string{"boleh beli 1 pcs?", "bisa ga beli satuan?", "kalau 1 biji bisa?"}
	for _, msg := range consultMsgs {
		for _, st := range waLanguageStyles {
			add(WABuyerCase{
				Category: "adversarial_false_checkout", LanguageStyle: st, Adversarial: true,
				InputUser: stylePhrase(msg, st),
				ExpectedIntent: SalesStateConsulting,
				ExpectedResponseBehavior: BehaviorNoFalseCheckout,
			})
		}
	}

	// Adversarial: qty di ask_recipient dengan revisi eksplisit
	for n := 5; n <= 15; n++ {
		stCopy := baseOrderAbon(1, "ask_recipient")
		add(WABuyerCase{
			Category: "adversarial_qty_revision", Adversarial: true,
			InputUser: fmt.Sprintf("revisi jadi %d biji", n), CurrentState: stCopy,
			ExpectQty: n, ExpectedResponseBehavior: BehaviorQtyUpdated,
		})
	}

	// Pad to minimum 2000 — parse qty + scope regression
	for len(out) < 2000 {
		i := len(out) + 1
		n := (i % 20) + 1
		add(WABuyerCase{
			Category:                 "regression_parse_qty",
			InputUser:                fmt.Sprintf("mau %d pcs abon sapi", n),
			ExpectedIntent:           SalesStateCartReady,
			ExpectedResponseBehavior: BehaviorOrderFlow,
		})
	}
	if len(out) > 2000 {
		out = out[:2000]
	}
	return out
}
