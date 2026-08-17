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

type paymentProofMeta = order.PaymentProofMeta

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
		return sendPaymentProofOutbound(ctx, conn, job,
			"Maaf kak, terlalu banyak pengiriman bukti transfer dalam 1 jam. Coba lagi nanti ya.")
	}

	target, payableCount, err := resolvePaymentTargetOrder(ctx, conn, job.TenantSchema, scope, job.InboundMessageID)
	if err != nil {
		return err
	}
	if target == nil {
		rlog.Info("payment proof skipped: no target order", "conversationId", job.ConversationID)
		return PublishImageContextJob(ctx, imageContextJobFromPaymentProof(job))
	}

	if handled, err := handleBlockedPaymentProof(ctx, conn, job, target); err != nil {
		return err
	} else if handled {
		return nil
	}

	imageBytes, mime, err := inbox.FetchMessageMediaBytes(ctx, job.TenantSchema, job.MessageID)
	if err != nil {
		return err
	}
	fileHash := sha256Hex(imageBytes)

	if otherID, dup, derr := lookupPaymentProofHashOtherOrder(ctx, conn, job.TenantSchema, fileHash, target.ID); derr != nil {
		return derr
	} else if dup {
		meta := paymentProofMeta{FileHash: fileHash, Flags: []string{"duplicate_hash"}}
		dupRef := order.FormatOrderNumber(otherID)
		return applyPaymentProofResult(ctx, conn, job, target, "rejected", target.Status, meta, false, dupRef)
	}

	settings, err := loadPaymentProfileSettings(ctx, conn)
	if err != nil {
		return err
	}

	allowed, _, _ := usage.CheckQuota(ctx, job.TenantSchema, "ai_token")
	if !allowed {
		meta := paymentProofMeta{FileHash: fileHash, Flags: []string{"quota_exceeded"}}
		return applyPaymentProofResult(ctx, conn, job, target, "proof_submitted", target.Status, meta, true, "")
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
		if settings.Mode == "auto_verify" {
			meta.Flags = appendPaymentProofFlag(meta.Flags, "ai_review_required")
		}
		return applyPaymentProofResult(ctx, conn, job, target, "proof_submitted", target.Status, meta, true, "")
	}

	autoVerify := false
	newStatus := target.Status
	if settings.Mode == "auto_verify" {
		if payableCount > 1 {
			meta.Flags = append(meta.Flags, "multi_order")
		}
		kb, _ := loadKBEntries(ctx, conn, 50)
		accounts := loadPaymentAccountsFromKB(kb)
		ok, flags := evaluatePaymentProofRules(target, ocr, accounts)
		meta.Flags = append(meta.Flags, flags...)
		minConf := settings.MinConf
		if minConf >= 1.0 {
			minConf = 0.95
		}
		if ok && ocr.Confidence >= minConf && len(accounts) > 0 {
			autoVerify = true
			newStatus = "processing"
		} else if ok && ocr.Confidence > 0 && ocr.Confidence < minConf {
			meta.Flags = appendPaymentProofFlag(meta.Flags, "low_confidence")
		}
	}

	paymentStatus := "proof_submitted"
	if autoVerify {
		paymentStatus = "verified"
	}
	meta.Flags = markPaymentProofAIReviewFlags(settings.Mode, autoVerify, paymentStatus, meta.Flags)
	return applyPaymentProofResult(ctx, conn, job, target, paymentStatus, newStatus, meta, autoVerify, "")
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
		if denied || o == nil || !isResolvableForPaymentProof(o) {
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
	if isOrderPaymentProofBlocked(o) {
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

func isResolvableForPaymentProof(o *persistedOrder) bool {
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

func isOrderPaymentProofBlocked(o *persistedOrder) bool {
	if o == nil {
		return false
	}
	return order.IsPaymentProofBlocked(order.ParsePaymentProofMeta(o.PaymentProofMetaJSON))
}

func handleBlockedPaymentProof(ctx context.Context, q tenantQuerier, job *PaymentProofJob, target *persistedOrder) (bool, error) {
	meta := order.ParsePaymentProofMeta(target.PaymentProofMetaJSON)
	if !order.IsPaymentProofBlocked(meta) {
		return false, nil
	}
	if meta.BlockedNotified {
		rlog.Info("payment proof ignored: order blocked", "orderId", target.ID)
		return true, nil
	}
	ref := order.FormatOrderNumber(target.ID)
	msg := fmt.Sprintf(
		"Maaf kak, bukti transfer untuk pesanan %s sudah ditolak %d kali. Silakan hubungi admin toko untuk bantuan.",
		ref, order.PaymentProofMaxRejections,
	)
	meta.BlockedNotified = true
	metaJSON, _ := json.Marshal(meta)
	_, err := q.ExecContext(ctx, fmt.Sprintf(`
		UPDATE "%s"."order" SET payment_proof_meta = $2::jsonb, updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL`, job.TenantSchema), target.ID, string(metaJSON))
	if err != nil {
		return true, err
	}
	if err := sendPaymentProofOutbound(ctx, q, job, msg); err != nil {
		return true, err
	}
	return true, nil
}

func mergePaymentProofMeta(existing, incoming paymentProofMeta) paymentProofMeta {
	out := incoming
	out.RejectionCount = existing.RejectionCount
	out.ProofBlocked = existing.ProofBlocked
	out.BlockedNotified = existing.BlockedNotified
	return out
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
	for _, kw := range []string{"bukti", "transfer", " tf", "tf ", "bayar", "pembayaran", "screenshot", "struk", "mutasi", "kirim lagi", "cek lagi", "kirim ulang", "coba cek"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	if strings.HasPrefix(text, "tf") || strings.HasSuffix(text, "tf") {
		return true
	}
	if parseOrderRefFromMessage(body) != "" {
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

func lookupPaymentProofHashOtherOrder(ctx context.Context, q tenantQuerier, tenantSchema, hash, orderID string) (string, bool, error) {
	if hash == "" {
		return "", false, nil
	}
	var otherID string
	err := q.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id::text FROM "%s"."order"
		WHERE deleted_at IS NULL
		  AND id <> $1::uuid
		  AND payment_proof_meta->>'fileHash' = $2
		LIMIT 1`, tenantSchema), orderID, hash).Scan(&otherID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return otherID, true, nil
}

func appendPaymentProofFlag(flags []string, flag string) []string {
	for _, f := range flags {
		if f == flag {
			return flags
		}
	}
	return append(flags, flag)
}

func markPaymentProofAIReviewFlags(mode string, autoVerify bool, paymentStatus string, flags []string) []string {
	if strings.ToLower(strings.TrimSpace(mode)) != "auto_verify" {
		return flags
	}
	if autoVerify || paymentStatus != "proof_submitted" {
		return flags
	}
	return appendPaymentProofFlag(flags, "ai_review_required")
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
		acctLine := line
		name := ""
		if idx := strings.Index(lower, "atas nama"); idx >= 0 {
			acctLine = strings.TrimSpace(line[:idx])
			name = strings.TrimSpace(line[idx+len("atas nama"):])
			name = strings.TrimPrefix(name, ":")
			name = strings.TrimSpace(name)
		} else if idx := strings.Index(lower, "a/n"); idx >= 0 {
			acctLine = strings.TrimSpace(line[:idx])
			name = strings.TrimSpace(line[idx+len("a/n"):])
			name = strings.TrimPrefix(name, ":")
			name = strings.TrimSpace(name)
		}
		acctNum := digitsOnly(acctLine)
		if len(acctNum) >= 8 && len(acctNum) <= 16 {
			out = append(out, paymentAccount{Bank: bank, AccountNumber: acctNum, AccountName: name})
			continue
		}
		if name != "" {
			out = append(out, paymentAccount{Bank: bank, AccountName: name})
		}
	}
	return out
}

func orderStatusNeedsStockPrecheck(currentStatus, newStatus string) bool {
	if strings.ToLower(strings.TrimSpace(newStatus)) != "processing" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(currentStatus)) {
	case "draft", "confirmed", "paid":
		return true
	default:
		return false
	}
}

func loadOrderStockItems(ctx context.Context, q tenantQuerier, tenantSchema, orderID string) ([]inventory.OrderStockItem, error) {
	var itemsRaw []byte
	if err := q.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(items, '[]') FROM "%s"."order" WHERE id = $1::uuid`, tenantSchema), orderID).Scan(&itemsRaw); err != nil {
		return nil, err
	}
	var items []order.OrderItem
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return nil, err
	}
	return aiOrderStockItems(items), nil
}

func applyPaymentProofResult(
	ctx context.Context,
	q tenantQuerier,
	job *PaymentProofJob,
	target *persistedOrder,
	paymentStatus, orderStatus string,
	meta paymentProofMeta,
	syncStock bool,
	duplicateOtherRef string,
) error {
	prevPaymentStatus := strings.ToLower(strings.TrimSpace(target.PaymentStatus))
	existing := order.ParsePaymentProofMeta(target.PaymentProofMetaJSON)
	meta = mergePaymentProofMeta(existing, meta)
	if paymentStatus == "rejected" {
		meta = order.IncrementPaymentRejection(meta)
	}
	metaJSON, _ := json.Marshal(meta)
	verifiedAtSQL := "NULL"
	verifiedBySQL := "NULL"
	if paymentStatus == "verified" {
		verifiedAtSQL = "NOW()"
	}

	stockItems, err := loadOrderStockItems(ctx, q, job.TenantSchema, target.ID)
	if err != nil {
		return err
	}
	if syncStock && orderStatusNeedsStockPrecheck(target.Status, orderStatus) {
		if err := inventory.PrecheckOrderStock(ctx, job.TenantSchema, target.ID, stockItems); err != nil {
			return err
		}
	}

	_, err = q.ExecContext(ctx, fmt.Sprintf(`
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
	body := paymentProofBuyerMessage(ref, paymentStatus, prevPaymentStatus, duplicateOtherRef)
	if body != "" {
		if err := sendPaymentProofOutbound(ctx, q, job, body); err != nil {
			return err
		}
	}

	if syncStock && orderStatus == "processing" {
		if err := inventory.SyncOrderStock(ctx, job.TenantSchema, target.ID, "processing", stockItems, ""); err != nil {
			return fmt.Errorf("payment proof stock sync: %w", err)
		}
	}

	if job.TenantID != "" && svc != nil && svc.rdb != nil {
		inboxrealtime.Publish(ctx, svc.rdb, job.TenantID)
	}
	return nil
}

func paymentProofBuyerMessage(ref, paymentStatus, prevPaymentStatus, duplicateOtherRef string) string {
	switch paymentStatus {
	case "proof_submitted":
		if prevPaymentStatus == "rejected" {
			return fmt.Sprintf("Bukti transfer baru untuk pesanan %s sudah kami terima. Tim akan mengecek ulang pembayaran.", ref)
		}
		return fmt.Sprintf("Bukti transfer untuk pesanan %s sudah kami terima. Tim akan memverifikasi pembayaran.", ref)
	case "verified":
		return fmt.Sprintf("Pembayaran pesanan %s terverifikasi. Pesanan sedang diproses.", ref)
	case "rejected":
		if duplicateOtherRef != "" {
			return fmt.Sprintf(
				"Maaf kak, bukti transfer untuk pesanan %s ditolak karena screenshot yang sama sudah dipakai untuk pesanan %s. Kirim bukti transfer yang sesuai pesanan ini ya.",
				ref, duplicateOtherRef,
			)
		}
		return fmt.Sprintf(
			"Maaf kak, bukti transfer untuk pesanan %s ditolak. Silakan kirim bukti yang benar dan sesuai total pesanan.",
			ref,
		)
	default:
		return ""
	}
}

func sendPaymentProofOutbound(ctx context.Context, q tenantQuerier, job *PaymentProofJob, text string) error {
	if svc == nil {
		return fmt.Errorf("ai service not initialized")
	}
	convo, err := loadConversation(ctx, q, job.ConversationID)
	if err != nil {
		return err
	}
	if convo == nil {
		return fmt.Errorf("conversation not found")
	}
	contact, err := loadContact(ctx, q, convo.ContactID)
	if err != nil {
		return err
	}
	channel, err := loadChannel(ctx, q, convo.ChannelID)
	if err != nil {
		return err
	}
	meta := metaNoLLM(reasonAIGenerated, PathPaymentProof)
	return svc.sendAiMessage(ctx, q, job.TenantID, convo, channel, contact, text, "system", meta)
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
