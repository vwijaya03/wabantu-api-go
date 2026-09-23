package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var tplSecrets struct {
	MidtransServerKey    string
	MidtransIsProduction string
}

type tplQRISResult struct {
	QRURL     string
	ExpiresAt time.Time
}

type tplChargeResp struct {
	Actions []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"actions"`
	StatusMessage string `json:"status_message"`
}

func tplMidtransBaseURL() string {
	if tplSecrets.MidtransIsProduction == "true" {
		return "https://api.midtrans.com"
	}
	return "https://api.sandbox.midtrans.com"
}

func chargeTemplateQRIS(ctx context.Context, orderID string, amount int64, desc string) (*tplQRISResult, error) {
	payload := map[string]any{
		"payment_type": "qris",
		"transaction_details": map[string]any{
			"order_id":     orderID,
			"gross_amount": amount,
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", tplMidtransBaseURL()+"/v2/charge", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(tplSecrets.MidtransServerKey, "")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var result tplChargeResp
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("midtrans %d: %s", resp.StatusCode, result.StatusMessage)
	}
	qr := ""
	for _, a := range result.Actions {
		if a.Name == "generate-qr-code" {
			qr = a.URL
			break
		}
	}
	return &tplQRISResult{QRURL: qr, ExpiresAt: time.Now().Add(15 * time.Minute)}, nil
}
