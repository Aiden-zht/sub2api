package provider

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const (
	xunhuPayHTTPTimeout     = 10 * time.Second
	maxXunhuPayResponseSize = 1 << 20
	xunhuPayStatusPaid      = "OD"
	xunhuPayStatusRefunded  = "CD"
	xunhuPayStatusRefunding = "RD"
	xunhuPayStatusRefundErr = "UD"
)

// XunhuPay implements payment.Provider for XunhuPay / HuPiJiao.
type XunhuPay struct {
	instanceID string
	config     map[string]string
	httpClient *http.Client
}

// NewXunhuPay creates a new XunhuPay provider.
// config keys: appId, appSecret, apiBase
func NewXunhuPay(instanceID string, config map[string]string) (*XunhuPay, error) {
	for _, k := range []string{"appId", "appSecret", "apiBase"} {
		if strings.TrimSpace(config[k]) == "" {
			return nil, fmt.Errorf("xunhupay config missing required key: %s", k)
		}
	}
	cfg := make(map[string]string, len(config))
	for k, v := range config {
		cfg[k] = v
	}
	cfg["apiBase"] = normalizeXunhuPayAPIBase(cfg["apiBase"])
	return &XunhuPay{
		instanceID: instanceID,
		config:     cfg,
		httpClient: &http.Client{Timeout: xunhuPayHTTPTimeout},
	}, nil
}

func (x *XunhuPay) Name() string        { return "XunhuPay" }
func (x *XunhuPay) ProviderKey() string { return payment.TypeXunhuPay }
func (x *XunhuPay) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAlipay, payment.TypeWxpay}
}

func (x *XunhuPay) MerchantIdentityMetadata() map[string]string {
	if x == nil {
		return nil
	}
	appID := strings.TrimSpace(x.config["appId"])
	if appID == "" {
		return nil
	}
	return map[string]string{"appid": appID}
}

func (x *XunhuPay) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	notifyURL, returnURL := x.resolveURLs(req)
	params := map[string]string{
		"version":          "1.1",
		"appid":            x.config["appId"],
		"trade_order_id":   req.OrderID,
		"payment":          x.paymentMethod(req.PaymentType),
		"total_fee":        req.Amount,
		"title":            req.Subject,
		"description":      req.Subject,
		"notify_url":       notifyURL,
		"return_url":       returnURL,
		"callback_url":     returnURL,
		"nonce_str":        strconv.FormatInt(time.Now().UnixNano(), 36),
		"time":             strconv.FormatInt(time.Now().Unix(), 10),
		"wap_name":         strings.TrimSpace(x.config["wapName"]),
		"spbill_create_ip": req.ClientIP,
	}
	params["hash"] = xunhuPaySign(params, x.config["appSecret"])

	body, err := x.post(ctx, x.endpoint("/payment/do.html"), params)
	if err != nil {
		return nil, fmt.Errorf("xunhupay create: %w", err)
	}
	var resp struct {
		ErrCode   any    `json:"errcode"`
		ErrMsg    string `json:"errmsg"`
		Hash      string `json:"hash"`
		URL       string `json:"url"`
		URLQRCode string `json:"url_qrcode"`
		CodeURL   string `json:"code_url"`
		QRCode    string `json:"qrcode"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("xunhupay parse: %w", err)
	}
	code, ok := xunhuPayErrCode(resp.ErrCode)
	if !ok {
		return nil, fmt.Errorf("xunhupay parse: missing errcode")
	}
	if code != 0 {
		msg := strings.TrimSpace(resp.ErrMsg)
		if msg == "" {
			msg = summarizeGatewayResponse(body)
		}
		return nil, fmt.Errorf("xunhupay error: %s", msg)
	}
	qrCode := firstNonEmpty(resp.URLQRCode, resp.CodeURL, resp.QRCode)
	if strings.TrimSpace(resp.URL) == "" && strings.TrimSpace(qrCode) == "" {
		return nil, fmt.Errorf("xunhupay create missing pay_url and qr_code")
	}
	return &payment.CreatePaymentResponse{TradeNo: req.OrderID, PayURL: resp.URL, QRCode: qrCode}, nil
}

func (x *XunhuPay) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	params := map[string]string{
		"appid":           x.config["appId"],
		"out_trade_order": tradeNo,
		"nonce_str":       strconv.FormatInt(time.Now().UnixNano(), 36),
		"time":            strconv.FormatInt(time.Now().Unix(), 10),
	}
	params["hash"] = xunhuPaySign(params, x.config["appSecret"])

	body, err := x.post(ctx, x.endpoint("/payment/query.html"), params)
	if err != nil {
		return nil, fmt.Errorf("xunhupay query: %w", err)
	}
	var resp struct {
		ErrCode any    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Data    struct {
			TradeOrderID  string `json:"trade_order_id"`
			OutTradeOrder string `json:"out_trade_order"`
			TransactionID string `json:"transaction_id"`
			OpenOrderID   string `json:"open_order_id"`
			TotalFee      string `json:"total_fee"`
			TotalAmount   string `json:"total_amount"`
			Status        string `json:"status"`
		} `json:"data"`
		TradeOrderID  string `json:"trade_order_id"`
		OutTradeOrder string `json:"out_trade_order"`
		TransactionID string `json:"transaction_id"`
		OpenOrderID   string `json:"open_order_id"`
		TotalFee      string `json:"total_fee"`
		TotalAmount   string `json:"total_amount"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("xunhupay parse query: %w", err)
	}
	code, ok := xunhuPayErrCode(resp.ErrCode)
	if !ok {
		return nil, fmt.Errorf("xunhupay parse query: missing errcode")
	}
	if code != 0 {
		msg := strings.TrimSpace(resp.ErrMsg)
		if msg == "" {
			msg = summarizeGatewayResponse(body)
		}
		return nil, fmt.Errorf("xunhupay query failed: %s", msg)
	}
	tradeOrderID := firstNonEmpty(resp.Data.TradeOrderID, resp.Data.OutTradeOrder, resp.TradeOrderID, resp.OutTradeOrder)
	transactionID := firstNonEmpty(resp.Data.TransactionID, resp.TransactionID, resp.Data.OpenOrderID, resp.OpenOrderID)
	totalAmount := firstNonEmpty(resp.Data.TotalAmount, resp.Data.TotalFee, resp.TotalAmount, resp.TotalFee)
	status := firstNonEmpty(resp.Data.Status, resp.Status)
	if strings.TrimSpace(status) == "" {
		return nil, fmt.Errorf("xunhupay query missing status")
	}
	if strings.TrimSpace(totalAmount) == "" {
		return nil, fmt.Errorf("xunhupay query missing total_amount")
	}
	amount, err := strconv.ParseFloat(totalAmount, 64)
	if err != nil {
		return nil, fmt.Errorf("xunhupay query invalid total_amount: %w", err)
	}
	return &payment.QueryOrderResponse{
		TradeNo:  firstNonEmpty(transactionID, tradeOrderID, tradeNo),
		Status:   xunhuPayOrderStatus(status),
		Amount:   amount,
		Metadata: x.MerchantIdentityMetadata(),
	}, nil
}

func (x *XunhuPay) VerifyNotification(_ context.Context, rawBody string, _ map[string]string) (*payment.PaymentNotification, error) {
	values, err := url.ParseQuery(rawBody)
	if err != nil {
		return nil, fmt.Errorf("parse notify: %w", err)
	}
	params := make(map[string]string, len(values))
	for k := range values {
		params[k] = values.Get(k)
	}
	sign := firstNonEmpty(params["hash"], params["sign"])
	if sign == "" {
		return nil, fmt.Errorf("missing hash")
	}
	if !xunhuPayVerifySign(params, x.config["appSecret"], sign) {
		return nil, fmt.Errorf("invalid signature")
	}
	if appID := strings.TrimSpace(params["appid"]); appID != "" && appID != strings.TrimSpace(x.config["appId"]) {
		return nil, fmt.Errorf("appid mismatch")
	}
	amount, _ := strconv.ParseFloat(params["total_fee"], 64)
	metadata := x.MerchantIdentityMetadata()
	if metadata == nil {
		metadata = map[string]string{}
	}
	if appID := strings.TrimSpace(params["appid"]); appID != "" {
		metadata["appid"] = appID
	}
	return &payment.PaymentNotification{
		TradeNo:  firstNonEmpty(params["transaction_id"], params["hash"]),
		OrderID:  firstNonEmpty(params["trade_order_id"], params["out_trade_no"]),
		Amount:   amount,
		Status:   xunhuPayNotificationStatus(params["status"]),
		RawData:  rawBody,
		Metadata: metadata,
	}, nil
}

func (x *XunhuPay) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	orderID := strings.TrimSpace(req.OrderID)
	if orderID == "" {
		return nil, fmt.Errorf("xunhupay refund missing trade_order_id")
	}
	params := map[string]string{
		"appid":          x.config["appId"],
		"trade_order_id": orderID,
		"reason":         strings.TrimSpace(req.Reason),
		"nonce_str":      strconv.FormatInt(time.Now().UnixNano(), 36),
		"time":           strconv.FormatInt(time.Now().Unix(), 10),
	}
	params["hash"] = xunhuPaySign(params, x.config["appSecret"])

	body, err := x.post(ctx, x.endpoint("/payment/refund.html"), params)
	if err != nil {
		return nil, fmt.Errorf("xunhupay refund: %w", err)
	}
	var resp struct {
		TradeOrderID  string `json:"trade_order_id"`
		TransactionID string `json:"transaction_id"`
		OutRefundNo   string `json:"out_refund_no"`
		RefundFee     string `json:"refund_fee"`
		Reason        string `json:"reason"`
		RefundStatus  string `json:"refund_status"`
		ErrCode       any    `json:"errcode"`
		ErrMsg        string `json:"errmsg"`
		Hash          string `json:"hash"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("xunhupay parse refund: %w", err)
	}
	code, ok := xunhuPayErrCode(resp.ErrCode)
	if !ok {
		return nil, fmt.Errorf("xunhupay parse refund: missing errcode")
	}
	if code != 0 {
		msg := strings.TrimSpace(resp.ErrMsg)
		if msg == "" {
			msg = summarizeGatewayResponse(body)
		}
		return nil, fmt.Errorf("xunhupay refund failed: %s", msg)
	}
	if strings.TrimSpace(resp.RefundStatus) == "" {
		return nil, fmt.Errorf("xunhupay refund missing refund_status")
	}
	if strings.TrimSpace(resp.Hash) == "" {
		return nil, fmt.Errorf("xunhupay refund missing hash")
	}
	responseParams := map[string]string{
		"trade_order_id": resp.TradeOrderID,
		"transaction_id": resp.TransactionID,
		"out_refund_no":  resp.OutRefundNo,
		"refund_fee":     resp.RefundFee,
		"reason":         resp.Reason,
		"refund_status":  resp.RefundStatus,
		"errcode":        strconv.Itoa(code),
		"errmsg":         resp.ErrMsg,
	}
	if !xunhuPayVerifySign(responseParams, x.config["appSecret"], resp.Hash) {
		return nil, fmt.Errorf("xunhupay refund invalid signature")
	}
	return &payment.RefundResponse{
		RefundID: firstNonEmpty(resp.OutRefundNo, resp.TransactionID, resp.TradeOrderID, req.OrderID),
		Status:   xunhuPayRefundStatus(resp.RefundStatus),
	}, nil
}

func (x *XunhuPay) resolveURLs(req payment.CreatePaymentRequest) (string, string) {
	notifyURL := firstNonEmpty(req.NotifyURL, x.config["notifyUrl"])
	returnURL := firstNonEmpty(req.ReturnURL, x.config["returnUrl"])
	return notifyURL, returnURL
}

func (x *XunhuPay) paymentMethod(paymentType string) string {
	if strings.HasPrefix(paymentType, payment.TypeWxpay) {
		return payment.TypeWxpay
	}
	return payment.TypeAlipay
}

func (x *XunhuPay) endpoint(path string) string {
	return strings.TrimRight(x.config["apiBase"], "/") + path
}

func (x *XunhuPay) post(ctx context.Context, endpoint string, params map[string]string) ([]byte, error) {
	form := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			form.Set(k, v)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := x.httpClient
	if client == nil {
		client = &http.Client{Timeout: xunhuPayHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxXunhuPayResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, summarizeGatewayResponse(body))
	}
	return body, nil
}

func normalizeXunhuPayAPIBase(apiBase string) string {
	base := strings.TrimSpace(apiBase)
	if base == "" {
		return ""
	}
	if parsed, err := url.Parse(base); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		parsed.RawPath = ""
		parsed.Path = trimXunhuPayEndpointPath(parsed.Path)
		return strings.TrimRight(parsed.String(), "/")
	}
	return strings.TrimRight(trimXunhuPayEndpointPath(base), "/")
}

func trimXunhuPayEndpointPath(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	lower := strings.ToLower(path)
	for _, endpoint := range []string{"/payment/do.html", "/payment/query.html", "/payment/refund.html"} {
		if strings.HasSuffix(lower, endpoint) {
			return strings.TrimRight(path[:len(path)-len(endpoint)], "/")
		}
	}
	return path
}

func xunhuPaySign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "hash" || k == "sign" || k == "sign_type" || strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	for i, k := range keys {
		if i > 0 {
			_ = buf.WriteByte('&')
		}
		_, _ = buf.WriteString(k + "=" + params[k])
	}
	_, _ = buf.WriteString(secret)
	sum := md5.Sum([]byte(buf.String()))
	return hex.EncodeToString(sum[:])
}

func xunhuPayVerifySign(params map[string]string, secret string, sign string) bool {
	return hmac.Equal([]byte(xunhuPaySign(params, secret)), []byte(sign))
}

func xunhuPayErrCode(code any) (int, bool) {
	switch v := code.(type) {
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	default:
		return 0, false
	}
}

func xunhuPayNotificationStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case xunhuPayStatusPaid, "PAID", "SUCCESS", "SUCCEEDED":
		return payment.ProviderStatusSuccess
	case xunhuPayStatusRefunded, xunhuPayStatusRefunding, xunhuPayStatusRefundErr, "FAILED", "FAIL", "CLOSED", "CANCELLED", "CANCELED":
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

func xunhuPayOrderStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case xunhuPayStatusPaid, "PAID", "SUCCESS", "SUCCEEDED":
		return payment.ProviderStatusPaid
	case xunhuPayStatusRefunded:
		return payment.ProviderStatusRefunded
	case xunhuPayStatusRefunding:
		return payment.ProviderStatusPending
	case xunhuPayStatusRefundErr, "FAILED", "FAIL", "CLOSED", "CANCELLED", "CANCELED":
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

func xunhuPayRefundStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case xunhuPayStatusRefunded:
		return payment.ProviderStatusSuccess
	case xunhuPayStatusRefunding, xunhuPayStatusPaid:
		return payment.ProviderStatusPending
	case xunhuPayStatusRefundErr, "FAILED", "FAIL", "CLOSED", "CANCELLED", "CANCELED":
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusFailed
	}
}

func summarizeGatewayResponse(body []byte) string {
	summary := strings.Join(strings.Fields(string(body)), " ")
	if summary == "" {
		return "<empty>"
	}
	if len(summary) > maxEasypayErrorSummary {
		return summary[:maxEasypayErrorSummary] + "..."
	}
	return summary
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
