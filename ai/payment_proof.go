package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/aivision"
	"encore.app/wabantu/inbox"
	"encore.app/wabantu/inventory"
	"encore.app/wabantu/order"
	"encore.app/wabantu/shared/inboxrealtime"
	"encore.app/wabantu/usage"
)

const (
	paymentProofRateLimitPerHour = 10
	paymentProofAmountTolerance  = 1000.0
	paymentProofRateKeyPrefix    = "payment_proof:rate:"
)

type paymentAccount struct {
	Bank          string
	AccountNumber string
	AccountName   string
}

type paymentProofMeta struct {
	Amount        *float64 `json:"amount,omitempty"`
	Bank          string   `json:"bank,omitempty"`
	AccountNumber string   `json:"accountNumber,omitempty"`
	AccountName   string   `json:"accountName,omitempty"`
	Date          string   `json:"date,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	Flags         []string `json:"flags,omitempty"`
	FileHash      string   `json:"fileHash,omitempty"`
}

type paymentProfileSettings struct {
	Mode       string
	MinConf    float64
}

var accountNumberRe = regexp.MustCompile(`\b\d{8,16}\b`)

func processPaymentProofJob(ctx context.Context, job *PaymentProofJob) error {
	conn, err := openTenantConn(ctx, job.TenantSchema)
	if err != nil {
		return err
	}
	defer conn.Close()

	scope := orderAccessScope{
		ConversationID: job.ConversationID,
		ContactID:      job.ContactID,
	}
	if !scope.valid() {
		return nil
	}

	if !checkPaymentProofRateLimit(ctx, job.TenantSchema, job.ContactID) {
		rlog.Info("payment proof rate limited", "contactId", job.ContactID)
		return nil
	}

	target, payableCount, err := resolvePaymentTargetOrder(ctx, conn, job.TenantSchema, scope, job.InboundMessageID)
	if err != nil {
		return err
	}
	if target == nil {
		rlog.Info("payment proof skipped: no target order", "conversationId", job.ConversationID)
		return nil
	}

	imageBytes, mime, err := inbox.FetchMessageMediaBytes(ctx, job.TenantSchema, job.MessageID)
	if err != nil {
		return err
	}
	fileHash := sha256Hex(imageBytes)

	if dup, derr := paymentProofHashUsedByOtherOrder(ctx, conn, job.TenantSchema, fileHash, target.ID); derr != nil {
		return derr
	} else if dup {
		meta := paymentProofMeta{FileHash: fileHash, Flags: []string{"duplicate_hash"}}
		return applyPaymentProofResult(ctx, conn, job, target, "rejected", target.Status, meta, false)
	}

	settings, err := loadPaymentProfileSettings(ctx, conn)
	if err != nil {
		return err
	}

	allowed, _, _ := usage.CheckQuota(ctx, job.TenantSchema, "ai_token")
	if !allowed {
		meta := paymentProofMeta{FileHash: fileHash, Flags: []string{"quota_exceeded"}}
		return applyPaymentProofResult(ctx, conn, job, target, "proof_submitted", target.Status, meta, true)
	}

	ocr, ocrUsage, ocrErr := aivision.ExtractPaymentProofFromImage(ctx, secrets.AnthropicApiKey, imageBytes, mime)
	_ = usage.RecordAIActivity(ctx, usage.AIActivityParams{
		TenantSchema:   job.TenantSchema,
		TenantID:       job.TenantID,
		ConversationID: job.ConversationID,
		InboundID:      job.InboundMessageID,
		Purpose:        usage.PurposePaymentProofOCR,
		Path:           "payment_proof_ocr",
		Model:          "claude-haiku-4-5",
		LLMUsed:        ocrErr == nil,
		InputTokens:    ocrUsage.InputTokens,
		OutputTokens:   ocrUsage.OutputTokens,
	})

	meta := paymentProofMetaFromOCR(ocr, fileHash)
	if ocrErr != nil {
		meta.Flags = append(meta.Flags, "ocr_failed")
		return applyPaymentProofResult(ctx, conn, job, target, "proof_submitted", target.Status, meta, true)
	}

	autoVerify := false
	newStatus := target.Status
	if settings.Mode == "auto_verify" && payableCount == 1 {
		kb, _ := loadKBEntries(ctx, conn, 50)
		accounts := loadPaymentAccountsFromKB(kb)
		ok, flags := evaluatePaymentProofRules(target, ocr, accounts)
		meta.Flags = append(meta.Flags, flags...)
		if ok && ocr.Confidence >= settings.MinConf && len(accounts) > 0 {
			autoVerify = true
			newStatus = "processing"
		}
	} else if payableCount > 1 {
		meta.Flags = append(meta.Flags, "multi_order")
	}

	paymentStatus := "proof_submitted"
	if autoVerify {
		paymentStatus = "verified"
	}
	return applyPaymentProofResult(ctx, conn, job, target, paymentStatus, newStatus, meta, autoVerify)
}

func resolvePaymentTargetOrder(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope, messageID string) (*persistedOrder, int, error) {
	count, err := countPayableOrdersForContact(ctx, q, tenantSchema, scope)
	if err != nil {
		return nil, 0, err
	}

	msg, _ := loadMessage(ctx, q, messageID)
	userText := ""
	if msg != nil {
		userText = strings.TrimSpace(msg.Body)
	}

	if ref := parseOrderRefFromMessage(userText); ref != "" {
		o, denied, err := loadOrderByRefForContact(ctx, q, tenantSchema, scope, ref)
		if err != nil {
			return nil, count, err
		}
		if denied || o == nil || !isPayablePaymentOrder(o) {
			return nil, count, nil
		}
		return o, count, nil
	}

	target, err := loadPaymentTargetOrder(ctx, q, tenantSchema, scope)
	if err != nil {
		return nil, 0, err
	}
	if target != nil {
		return target, count, nil
	}
	history, _ := loadHistory(ctx, q, scope.ConversationID, 12)
	if !IsActiveCheckoutFromHistory(history, userText) {
		return nil, count, nil
	}
	latest, err := loadLatestOrderForContact(ctx, q, tenantSchema, scope)
	if err != nil || latest == nil {
		return nil, count, err
	}
	if !isPayablePaymentOrder(latest) {
		return nil, count, nil
	}
	return latest, count, nil
}

func isPayablePaymentOrder(o *persistedOrder) bool {
	if o == nil {
		return false
	}
	if o.Status == "cancelled" || o.Status == "completed" {
		return false
	}
	pay := strings.ToLower(strings.TrimSpace(o.PaymentStatus))
	if pay == "" {
		pay = "unpaid"
	}
	switch pay {
	case "unpaid", "proof_submitted", "rejected":
		return true
	default:
		return false
	}
}

// IsPaymentProofInbound — image (or caption) that should be handled by payment-proof pipeline only.
func IsPaymentProofInbound(msgType, body string) bool {
	if !strings.EqualFold(strings.TrimSpace(msgType), "image") {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(body))
	if text == "" {
		return true
	}
	for _, kw := range []string{"bukti", "transfer", " tf", "tf ", "bayar", "pembayaran", "screenshot", "struk", "mutasi"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	if strings.HasPrefix(text, "tf") || strings.HasSuffix(text, "tf") {
		return true
	}
	if parseOrderRefFromMessage(body) != "" &&
		(strings.Contains(text, "bayar") || strings.Contains(text, "bukti")) {
		return true
	}
	return false
}

func loadPaymentTargetOrder(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) (*persistedOrder, error) {
	if !scope.valid() {
		return nil, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	row := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status IN ('draft','processing','confirmed','paid')
		  AND payment_status IN ('unpaid','proof_submitted','rejected')%s
		ORDER BY created_at DESC
		LIMIT 1`, persistedOrderSelectCols, tenantSchema, owner), scope.ConversationID, scope.ContactID)
	o, err := scanPersistedOrderRow(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return o, err
}

func countPayableOrdersForContact(ctx context.Context, q tenantQuerier, tenantSchema string, scope orderAccessScope) (int, error) {
	if !scope.valid() {
		return 0, nil
	}
	owner := sqlOrderOwnerFilter(1, 2)
	var n int
	err := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM "%s"."order"
		WHERE conversation_id = $1::uuid AND deleted_at IS NULL
		  AND status IN ('draft','processing','confirmed','paid')
		  AND payment_status IN ('unpaid','proof_submitted','rejected')%s`,
		tenantSchema, owner), scope.ConversationID, scope.ContactID).Scan(&n)
	return n, err
}

func loadPaymentProfileSettings(ctx context.Context, q tenantQuerier) (paymentProfileSettings, error) {
	var mode string
	var minConf float64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(payment_verification_mode, 'manual'),
		       COALESCE(payment_auto_verify_min_confidence, 0.95)
		FROM business_profile LIMIT 1`).Scan(&mode, &minConf)
	if err == sql.ErrNoRows {
		return paymentProfileSettings{Mode: "manual", MinConf: 0.95}, nil
	}
	if err != nil {
		return paymentProfileSettings{}, err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "auto_verify" {
		mode = "manual"
	}
	return paymentProfileSettings{Mode: mode, MinConf: minConf}, nil
}

func paymentProofHashUsedByOtherOrder(ctx context.Context, q tenantQuerier, tenantSchema, hash, orderID string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	var otherID string
	err := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id::text FROM "%s"."order"
		WHERE deleted_at IS NULL
		  AND id <> $1::uuid
		  AND payment_proof_meta->>'fileHash' = $2
		LIMIT 1`, tenantSchema), orderID, hash).Scan(&otherID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func paymentProofMetaFromOCR(ocr aivision.PaymentProofExtract, fileHash string) paymentProofMeta {
	meta := paymentProofMeta{
		Bank:          strings.TrimSpace(ocr.Bank),
		AccountNumber: strings.TrimSpace(ocr.AccountNumber),
		AccountName:   strings.TrimSpace(ocr.AccountName),
		Date:          strings.TrimSpace(ocr.Date),
		Confidence:    ocr.Confidence,
		FileHash:      fileHash,
	}
	if ocr.Amount > 0 {
		amt := ocr.Amount
		meta.Amount = &amt
	}
	return meta
}

func evaluatePaymentProofRules(target *persistedOrder, ocr aivision.PaymentProofExtract, accounts []paymentAccount) (bool, []string) {
	var flags []string
	if target == nil {
		return false, []string{"no_order"}
	}
	if len(accounts) == 0 {
		return false, []string{"kb_empty"}
	}
	if ocr.Amount <= 0 {
		flags = append(flags, "missing_amount")
	} else if math.Abs(ocr.Amount-target.Total) > paymentProofAmountTolerance {
		flags = append(flags, "mismatch_amount")
	}
	if !paymentDateValid(ocr.Date) {
		flags = append(flags, "invalid_date")
	}
	if !accountMatchesKB(ocr, accounts) {
		flags = append(flags, "account_mismatch")
	}
	for _, f := range flags {
		switch f {
		case "mismatch_amount", "account_mismatch", "missing_amount", "invalid_date", "kb_empty":
			return false, flags
		}
	}
	return true, flags
}

func paymentDateValid(date string) bool {
	date = strings.TrimSpace(date)
	if date == "" {
		return true
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return true
	}
	now := time.Now()
	return !t.After(now.Add(24 * time.Hour))
}

func accountMatchesKB(ocr aivision.PaymentProofExtract, accounts []paymentAccount) bool {
	ocrAcct := digitsOnly(ocr.AccountNumber)
	ocrBank := strings.ToLower(strings.TrimSpace(ocr.Bank))
	ocrName := strings.ToLower(strings.TrimSpace(ocr.AccountName))
	for _, a := range accounts {
		kbAcct := digitsOnly(a.AccountNumber)
		if kbAcct != "" && ocrAcct != "" && kbAcct == ocrAcct {
			return true
		}
		kbBank := strings.ToLower(strings.TrimSpace(a.Bank))
		if kbBank != "" && ocrBank != "" && (strings.Contains(ocrBank, kbBank) || strings.Contains(kbBank, ocrBank)) {
			if ocrName != "" && a.AccountName != "" && namesLooselyMatch(ocrName, a.AccountName) {
				return true
			}
			if ocrAcct == "" && kbAcct == "" {
				return true
			}
		}
		if a.AccountName != "" && ocrName != "" && namesLooselyMatch(ocrName, a.AccountName) {
			if kbAcct == "" || ocrAcct == "" || kbAcct == ocrAcct {
				return true
			}
		}
	}
	return false
}

func namesLooselyMatch(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isPaymentKBEntry(question, category, answer string) bool {
	question = strings.ToLower(question)
	category = strings.ToLower(category)
	answer = strings.ToLower(answer)
	tags := []string{"payment", "rekening", "bank", "transfer", "pembayaran"}
	for _, t := range tags {
		if strings.Contains(question, t) || strings.Contains(category, t) || strings.Contains(answer, t) {
			return true
		}
	}
	return strings.Contains(question, "rekening") || strings.Contains(question, "transfer")
}

func loadPaymentAccountsFromKB(kb []dbKBEntry) []paymentAccount {
	var out []paymentAccount
	seen := map[string]bool{}
	for _, e := range kb {
		if !e.IsActive {
			continue
		}
		cat := ""
		if e.Category != nil {
			cat = *e.Category
		}
		if !isPaymentKBEntry(e.Question, cat, e.Answer) {
			continue
		}
		for _, acct := range parsePaymentAccountsFromText(e.Answer) {
			key := acct.Bank + "|" + acct.AccountNumber + "|" + acct.AccountName
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, acct)
		}
	}
	return out
}

func parsePaymentAccountsFromText(text string) []paymentAccount {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var out []paymentAccount
	for _, num := range accountNumberRe.FindAllString(text, -1) {
		out = append(out, paymentAccount{AccountNumber: num})
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		bank := ""
		for _, b := range []string{"bca", "mandiri", "bri", "bni", "btn", "cimb", "jenius", "gopay", "ovo", "dana", "seabank"} {
			if strings.Contains(lower, b) {
				bank = strings.ToUpper(b)
				break
			}
		}
		if strings.Contains(lower, "atas nama") || strings.Contains(lower, "a/n") {
			parts := strings.SplitN(line, ":", 2)
			name := strings.TrimSpace(parts[len(parts)-1])
			if name != "" {
				out = append(out, paymentAccount{Bank: bank, AccountName: name})
			}
		}
	}
	return out
}

func applyPaymentProofResult(
	ctx context.Context,
	q tenantQuerier,
	job *PaymentProofJob,
	target *persistedOrder,
	paymentStatus, orderStatus string,
	meta paymentProofMeta,
	syncStock bool,
) error {
	metaJSON, _ := json.Marshal(meta)
	verifiedAtSQL := "NULL"
	verifiedBySQL := "NULL"
	if paymentStatus == "verified" {
		verifiedAtSQL = "NOW()"
	}
	_, err := q.ExecContext(ctx, fmt.Sprintf(`
		UPDATE "%s"."order" SET
			payment_status = $2,
			status = $3,
			payment_proof_message_id = $4::uuid,
			payment_proof_submitted_at = COALESCE(payment_proof_submitted_at, NOW()),
			payment_proof_verified_at = %s,
			payment_proof_verified_by = %s,
			payment_proof_meta = $5::jsonb,
			updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL`,
		job.TenantSchema, verifiedAtSQL, verifiedBySQL),
		target.ID, paymentStatus, orderStatus, job.MessageID, string(metaJSON))
	if err != nil {
		return err
	}

	ref := order.FormatOrderNumber(target.ID)
	if paymentStatus == "proof_submitted" {
		insertPaymentProofSystemMessage(ctx, q, job.ConversationID,
			fmt.Sprintf("Bukti transfer diterima untuk pesanan %s. Tim akan memverifikasi pembayaran.", ref))
	} else if paymentStatus == "verified" {
		insertPaymentProofSystemMessage(ctx, q, job.ConversationID,
			fmt.Sprintf("Pembayaran pesanan %s terverifikasi. Pesanan sedang diproses.", ref))
	} else if paymentStatus == "rejected" {
		insertPaymentProofSystemMessage(ctx, q, job.ConversationID,
			fmt.Sprintf("Bukti transfer untuk pesanan %s ditolak (duplikat atau tidak valid). Silakan kirim bukti yang benar.", ref))
	}

	if syncStock && orderStatus == "processing" {
		var itemsRaw []byte
		_ = q.QueryRowContext(ctx, fmt.Sprintf(
			`SELECT COALESCE(items, '[]') FROM "%s"."order" WHERE id = $1::uuid`, job.TenantSchema), target.ID).Scan(&itemsRaw)
		var items []order.OrderItem
		_ = json.Unmarshal(itemsRaw, &items)
		stockItems := make([]inventory.OrderStockItem, 0, len(items))
		for _, it := range items {
			stockItems = append(stockItems, inventory.OrderStockItem{
				LineID: it.LineID, CatalogItemID: it.CatalogItemID,
				WarehouseID: it.WarehouseID, Qty: it.Qty,
			})
		}
		if err := inventory.SyncOrderStock(ctx, job.TenantSchema, target.ID, "processing", stockItems, ""); err != nil {
			rlog.Warn("payment proof stock sync failed", "orderId", target.ID, "err", err)
		}
	}

	if job.TenantID != "" {
		inboxrealtime.Publish(ctx, svc.rdb, job.TenantID)
	}
	return nil
}

func insertPaymentProofSystemMessage(ctx context.Context, q tenantQuerier, conversationID, body string) {
	_, _ = q.ExecContext(ctx, `
		INSERT INTO message (conversation_id, direction, author, type, body, metadata, status)
		VALUES ($1, 'out', 'system', 'text', $2, '{}'::jsonb, 'sent')`,
		conversationID, body)
}

func checkPaymentProofRateLimit(ctx context.Context, tenantSchema, contactID string) bool {
	if svc == nil || svc.rdb == nil {
		return true
	}
	key := paymentProofRateKeyPrefix + tenantSchema + ":" + contactID
	n, err := svc.rdb.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		_ = svc.rdb.Expire(ctx, key, time.Hour).Err()
	}
	return n <= paymentProofRateLimitPerHour
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
