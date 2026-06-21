package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestXunhuPaySignExcludesSignatureFieldsAndEmptyValues(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"appid":          "app_1001",
		"trade_order_id": "ORDER123",
		"total_fee":      "10.00",
	}
	withIgnored := map[string]string{
		"appid":          "app_1001",
		"trade_order_id": "ORDER123",
		"total_fee":      "10.00",
		"hash":           "ignored",
		"sign":           "ignored",
		"empty":          "",
	}

	if got, want := xunhuPaySign(withIgnored, "secret"), xunhuPaySign(base, "secret"); got != want {
		t.Fatalf("signature should ignore hash/sign/empty fields: got %q want %q", got, want)
	}
}

func TestXunhuPayCreatePaymentParsesHostedURLAndQRCode(t *testing.T) {
	t.Parallel()

	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/payment/do.html" {
			t.Fatalf("path = %q, want /payment/do.html", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		received = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errcode":    0,
			"errmsg":     "success",
			"hash":       "xhp_hash_1",
			"url":        "https://pay.example/mobile-checkout",
			"url_qrcode": "https://pay.example/pc-qrcode.png",
		})
	}))
	defer server.Close()

	provider, err := NewXunhuPay("inst-1", map[string]string{
		"appId":     "app_1001",
		"appSecret": "secret",
		"apiBase":   server.URL,
	})
	if err != nil {
		t.Fatalf("NewXunhuPay: %v", err)
	}

	resp, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID:     "ORDER123",
		Amount:      "10.00",
		PaymentType: payment.TypeWxpay,
		Subject:     "Test Product",
		NotifyURL:   "https://merchant.example/notify",
		ReturnURL:   "https://merchant.example/return",
		ClientIP:    "127.0.0.1",
		IsMobile:    true,
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}

	if resp.TradeNo != "ORDER123" {
		t.Fatalf("TradeNo = %q, want ORDER123", resp.TradeNo)
	}
	if resp.PayURL != "https://pay.example/mobile-checkout" {
		t.Fatalf("PayURL = %q", resp.PayURL)
	}
	if resp.QRCode != "https://pay.example/pc-qrcode.png" {
		t.Fatalf("QRCode = %q", resp.QRCode)
	}
	if received.Get("appid") != "app_1001" || received.Get("trade_order_id") != "ORDER123" {
		t.Fatalf("request form missing merchant/order fields: %v", received)
	}
	if received.Get("hash") == "" {
		t.Fatalf("request form missing hash signature: %v", received)
	}
}

func TestXunhuPayCreatePaymentDoesNotUseMobileURLAsQRCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errcode": 0,
			"errmsg":  "success",
			"hash":    "xhp_hash_2",
			"url":     "https://pay.example/mobile-only",
		})
	}))
	defer server.Close()

	provider, err := NewXunhuPay("inst-1", map[string]string{
		"appId":       "app_1001",
		"appSecret":   "secret",
		"apiBase":     server.URL,
		"paymentMode": "qrcode",
	})
	if err != nil {
		t.Fatalf("NewXunhuPay: %v", err)
	}

	resp, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID:     "ORDER124",
		Amount:      "10.00",
		PaymentType: payment.TypeWxpay,
		Subject:     "Test Product",
		NotifyURL:   "https://merchant.example/notify",
		ReturnURL:   "https://merchant.example/return",
		ClientIP:    "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if resp.PayURL != "https://pay.example/mobile-only" {
		t.Fatalf("PayURL = %q", resp.PayURL)
	}
	if resp.QRCode != "" {
		t.Fatalf("QRCode = %q, want empty when url_qrcode is absent", resp.QRCode)
	}
}

func TestXunhuPayCreatePaymentRejectsMissingErrCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errmsg": "success",
			"url":    "https://pay.example/mobile-only",
		})
	}))
	defer server.Close()

	provider, err := NewXunhuPay("inst-1", map[string]string{
		"appId":     "app_1001",
		"appSecret": "secret",
		"apiBase":   server.URL,
	})
	if err != nil {
		t.Fatalf("NewXunhuPay: %v", err)
	}

	_, err = provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID:     "ORDER125",
		Amount:      "10.00",
		PaymentType: payment.TypeWxpay,
		Subject:     "Test Product",
		NotifyURL:   "https://merchant.example/notify",
		ReturnURL:   "https://merchant.example/return",
		ClientIP:    "127.0.0.1",
	})
	if err == nil || !strings.Contains(err.Error(), "missing errcode") {
		t.Fatalf("CreatePayment err = %v, want missing errcode", err)
	}
}

func TestXunhuPayVerifyNotification(t *testing.T) {
	t.Parallel()

	params := map[string]string{
		"appid":          "app_1001",
		"trade_order_id": "ORDER123",
		"transaction_id": "XHP_TXN_1",
		"total_fee":      "10.00",
		"status":         "OD",
	}
	params["hash"] = xunhuPaySign(params, "secret")

	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}

	provider, err := NewXunhuPay("inst-1", map[string]string{
		"appId":     "app_1001",
		"appSecret": "secret",
		"apiBase":   "https://api.example",
	})
	if err != nil {
		t.Fatalf("NewXunhuPay: %v", err)
	}

	notification, err := provider.VerifyNotification(context.Background(), values.Encode(), nil)
	if err != nil {
		t.Fatalf("VerifyNotification: %v", err)
	}

	if notification.OrderID != "ORDER123" {
		t.Fatalf("OrderID = %q", notification.OrderID)
	}
	if notification.TradeNo != "XHP_TXN_1" {
		t.Fatalf("TradeNo = %q", notification.TradeNo)
	}
	if notification.Status != payment.ProviderStatusSuccess {
		t.Fatalf("Status = %q", notification.Status)
	}
	if notification.Amount != 10 {
		t.Fatalf("Amount = %v", notification.Amount)
	}
}

func TestXunhuPayQueryOrderMapsPaidStatus(t *testing.T) {
	t.Parallel()

	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/payment/query.html") {
			t.Fatalf("path = %q, want suffix /payment/query.html", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		received = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errcode": 0,
			"data": map[string]any{
				"out_trade_order": "ORDER123",
				"open_order_id":   "20300634659",
				"transaction_id":  "XHP_TXN_1",
				"total_amount":    "10.00",
				"status":          "OD",
			},
		})
	}))
	defer server.Close()

	provider, err := NewXunhuPay("inst-1", map[string]string{
		"appId":     "app_1001",
		"appSecret": "secret",
		"apiBase":   server.URL,
	})
	if err != nil {
		t.Fatalf("NewXunhuPay: %v", err)
	}

	resp, err := provider.QueryOrder(context.Background(), "ORDER123")
	if err != nil {
		t.Fatalf("QueryOrder: %v", err)
	}

	if resp.TradeNo != "XHP_TXN_1" {
		t.Fatalf("TradeNo = %q", resp.TradeNo)
	}
	if resp.Status != payment.ProviderStatusPaid {
		t.Fatalf("Status = %q", resp.Status)
	}
	if resp.Amount != 10 {
		t.Fatalf("Amount = %v", resp.Amount)
	}
	if received.Get("out_trade_order") != "ORDER123" {
		t.Fatalf("out_trade_order = %q, want ORDER123; form=%v", received.Get("out_trade_order"), received)
	}
	if received.Get("trade_order_id") != "" {
		t.Fatalf("trade_order_id should not be sent for query; form=%v", received)
	}
}

func TestXunhuPayQueryOrderRejectsMissingErrCode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"out_trade_order": "ORDER123",
				"total_amount":    "10.00",
				"status":          "OD",
			},
		})
	}))
	defer server.Close()

	provider, err := NewXunhuPay("inst-1", map[string]string{
		"appId":     "app_1001",
		"appSecret": "secret",
		"apiBase":   server.URL,
	})
	if err != nil {
		t.Fatalf("NewXunhuPay: %v", err)
	}

	_, err = provider.QueryOrder(context.Background(), "ORDER123")
	if err == nil || !strings.Contains(err.Error(), "missing errcode") {
		t.Fatalf("QueryOrder err = %v, want missing errcode", err)
	}
}

func TestXunhuPayRefundUsesTradeOrderIDAndMapsPendingStatus(t *testing.T) {
	t.Parallel()

	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/payment/refund.html" {
			t.Fatalf("path = %q, want /payment/refund.html", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		received = r.PostForm
		payload := map[string]string{
			"trade_order_id": "ORDER123",
			"transaction_id": "XHP_TXN_1",
			"out_refund_no":  "REFUND_123",
			"refund_fee":     "10.00",
			"reason":         "customer request",
			"refund_status":  "RD",
			"errcode":        "0",
			"errmsg":         "",
		}
		payload["hash"] = xunhuPaySign(payload, "secret")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	provider, err := NewXunhuPay("inst-1", map[string]string{
		"appId":     "app_1001",
		"appSecret": "secret",
		"apiBase":   server.URL,
	})
	if err != nil {
		t.Fatalf("NewXunhuPay: %v", err)
	}

	resp, err := provider.Refund(context.Background(), payment.RefundRequest{
		OrderID: "ORDER123",
		Reason:  "customer request",
	})
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if resp.RefundID != "REFUND_123" {
		t.Fatalf("RefundID = %q", resp.RefundID)
	}
	if resp.Status != payment.ProviderStatusPending {
		t.Fatalf("Status = %q, want pending", resp.Status)
	}
	if received.Get("trade_order_id") != "ORDER123" {
		t.Fatalf("trade_order_id = %q, want ORDER123", received.Get("trade_order_id"))
	}
	if received.Get("open_order_id") != "" {
		t.Fatalf("open_order_id should not be sent; form=%v", received)
	}
	if received.Get("hash") == "" {
		t.Fatalf("request form missing hash signature: %v", received)
	}
}
