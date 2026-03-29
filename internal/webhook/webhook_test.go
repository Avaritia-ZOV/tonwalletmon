package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"ton-monitoring/internal/domain"
)

// ---------------------------------------------------------------------------
// Builder tests
// ---------------------------------------------------------------------------

func TestBuildPayload_Format(t *testing.T) {
	tx := domain.Transaction{
		AccountID:  "0:abc123",
		TxHash:     "deadbeef",
		Sender:     "EQSender123",
		SenderName: "Rapira",
		Recipient:  "EQRecipient456",
		Comment:    "hello",
		ActionType: "Ton Transfer",
		Value:      "1.5 TON",
		Lt:         42,
		Timestamp:  1700000000,
		Amount:     1500000000,
	}

	buf := make([]byte, 0, 512)
	payload := BuildPayload(buf, tx)

	// Must be valid JSON.
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v\npayload: %s", err, payload)
	}

	// Verify all fields present and correct.
	if got := m["event"]; got != "transaction" {
		t.Errorf("event = %v, want transaction", got)
	}
	if got := m["account_id"]; got != "0:abc123" {
		t.Errorf("account_id = %v, want 0:abc123", got)
	}
	if got := m["tx_hash"]; got != "deadbeef" {
		t.Errorf("tx_hash = %v, want deadbeef", got)
	}
	if got := m["action"]; got != "Ton Transfer" {
		t.Errorf("action = %v, want Ton Transfer", got)
	}
	if got := m["value"]; got != "1.5 TON" {
		t.Errorf("value = %v, want 1.5 TON", got)
	}
	if got := m["sender"]; got != "EQSender123" {
		t.Errorf("sender = %v, want EQSender123", got)
	}
	if got := m["sender_name"]; got != "Rapira" {
		t.Errorf("sender_name = %v, want Rapira", got)
	}
	if got := m["recipient"]; got != "EQRecipient456" {
		t.Errorf("recipient = %v, want EQRecipient456", got)
	}
	if got := m["comment"]; got != "hello" {
		t.Errorf("comment = %v, want hello", got)
	}
	// JSON numbers decode as float64.
	if got, ok := m["amount_nano"].(float64); !ok || got != 1500000000 {
		t.Errorf("amount_nano = %v, want 1500000000", m["amount_nano"])
	}
	if got, ok := m["lt"].(float64); !ok || got != 42 {
		t.Errorf("lt = %v, want 42", m["lt"])
	}
	if got, ok := m["timestamp"].(float64); !ok || got != 1700000000 {
		t.Errorf("timestamp = %v, want 1700000000", m["timestamp"])
	}

	// Exactly 12 fields: event, account_id, tx_hash, action, value, sender, sender_name, recipient, amount_nano, comment, lt, timestamp.
	if len(m) != 12 {
		t.Errorf("field count = %d, want 12; fields: %v", len(m), m)
	}
}

func TestBuildPayload_SpecialChars(t *testing.T) {
	// Comment and SenderName with JSON-special characters: double quote, backslash, newlines.
	tx := domain.Transaction{
		AccountID:  "0:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		TxHash:     "aabbccdd:eeff0011",
		Sender:     "EQBsender",
		SenderName: "Alice \"The Dev\"\nLine2\\End",
		Recipient:  "EQBrecipient",
		Amount:     100_000_000, // 0.1 TON
		ActionType: "Ton Transfer",
		Value:      "0.1 TON",
		Comment:    "He said \"hello\\\" and left\nbye",
		Lt:         999999999999,
		Timestamp:  -1,
	}

	buf := make([]byte, 0, 512)
	payload := BuildPayload(buf, tx)

	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("invalid JSON with special chars: %v\npayload: %s", err, payload)
	}

	if got := m["account_id"]; got != tx.AccountID {
		t.Errorf("account_id = %v, want %s", got, tx.AccountID)
	}
	if got := m["tx_hash"]; got != tx.TxHash {
		t.Errorf("tx_hash = %v, want %s", got, tx.TxHash)
	}
	if got := m["comment"]; got != tx.Comment {
		t.Errorf("comment = %v, want %s", got, tx.Comment)
	}
	if got := m["sender_name"]; got != tx.SenderName {
		t.Errorf("sender_name = %v, want %s", got, tx.SenderName)
	}
	if got := m["action"]; got != "Ton Transfer" {
		t.Errorf("action = %v, want Ton Transfer", got)
	}
	if got := m["value"]; got != "0.1 TON" {
		t.Errorf("value = %v, want 0.1 TON", got)
	}
	if got, ok := m["timestamp"].(float64); !ok || got != -1 {
		t.Errorf("timestamp = %v, want -1", m["timestamp"])
	}
}

func TestBuildPayload_EmptyComment(t *testing.T) {
	tx := domain.Transaction{
		AccountID:  "0:abc",
		TxHash:     "aabb",
		Sender:     "EQBsender",
		Recipient:  "EQBrecipient",
		Amount:     0,
		ActionType: "",
		Value:      "",
		Comment:    "",
		Lt:         1,
		Timestamp:  1700000000,
	}

	buf := make([]byte, 0, 512)
	payload := BuildPayload(buf, tx)

	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v\npayload: %s", err, payload)
	}

	if got := m["comment"]; got != "" {
		t.Errorf("comment = %v, want empty string", got)
	}
	if got := m["action"]; got != "" {
		t.Errorf("action = %v, want empty string", got)
	}
	if got := m["value"]; got != "" {
		t.Errorf("value = %v, want empty string", got)
	}
}

func TestBuildPayload_EmptySender(t *testing.T) {
	tx := domain.Transaction{
		AccountID:  "0:abc",
		TxHash:     "aabb",
		Sender:     "",
		SenderName: "",
		Recipient:  "EQBrecipient",
		Amount:     1_000_000_000,
		ActionType: "Ton Transfer",
		Value:      "1 TON",
		Comment:    "test",
		Lt:         1,
		Timestamp:  1700000000,
	}

	buf := make([]byte, 0, 512)
	payload := BuildPayload(buf, tx)

	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v\npayload: %s", err, payload)
	}

	if got := m["sender"]; got != "" {
		t.Errorf("sender = %v, want empty string", got)
	}
	if got := m["sender_name"]; got != "" {
		t.Errorf("sender_name = %v, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// appendJSONString tests
// ---------------------------------------------------------------------------

func TestAppendJSONString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{`say "hi"`, `say \"hi\"`},
		{`back\slash`, `back\\slash`},
		{"line\nbreak", `line\nbreak`},
		{"tab\there", `tab\there`},
		{"cr\rhere", `cr\rhere`},
		{"mixed\"\\\n\r\t", `mixed\"\\\n\r\t`},
		{"", ""},
	}

	for _, tt := range tests {
		buf := appendJSONString(nil, tt.input)
		got := string(buf)
		if got != tt.want {
			t.Errorf("appendJSONString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func BenchmarkBuildPayload(b *testing.B) {
	tx := domain.Transaction{
		AccountID:  "0:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		TxHash:     "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		Sender:     "EQBvW8Z5huBkMJYdnfAEM5JqTNkuWX3diqYENkWsIL0XggGG",
		SenderName: "Rapira",
		Recipient:  "EQD__________________________________________0vo",
		Amount:     1_500_000_000,
		ActionType: "Ton Transfer",
		Value:      "1.5 TON",
		Comment:    "Payment for services",
		Lt:         48723948723,
		Timestamp:  1700000000,
	}

	// Pre-allocate buffer from pool to prove zero-alloc in steady state.
	bp := acquireBuffer()
	defer releaseBuffer(bp)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		*bp = (*bp)[:0]
		*bp = BuildPayload(*bp, tx)
	}

	// Verify the output is non-empty so the compiler does not optimize away.
	if len(*bp) == 0 {
		b.Fatal("empty payload")
	}
}

// ---------------------------------------------------------------------------
// Signer tests
// ---------------------------------------------------------------------------

func TestSign_KnownVector(t *testing.T) {
	secret := []byte("test-secret-key")
	InitHMACPool(secret)

	payload := []byte(`{"event":"transaction","account_id":"0:abc123","tx_hash":"deadbeef","lt":42,"timestamp":1700000000}`)

	// Compute expected HMAC-SHA256 with stdlib directly.
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	dst := make([]byte, 64)
	n := Sign(dst, payload)

	if n != 64 {
		t.Fatalf("Sign returned %d bytes, want 64", n)
	}

	got := string(dst[:n])
	if got != expected {
		t.Errorf("signature mismatch:\n  got  = %s\n  want = %s", got, expected)
	}
}

func TestSign_Consistency(t *testing.T) {
	secret := []byte("consistency-key")
	InitHMACPool(secret)

	payload := []byte(`{"data":"same-payload-every-time"}`)
	dst1 := make([]byte, 64)
	dst2 := make([]byte, 64)

	Sign(dst1, payload)
	Sign(dst2, payload)

	if string(dst1) != string(dst2) {
		t.Errorf("inconsistent signatures:\n  first  = %s\n  second = %s", dst1, dst2)
	}

	// Third call to exercise pool recycling.
	dst3 := make([]byte, 64)
	Sign(dst3, payload)
	if string(dst1) != string(dst3) {
		t.Errorf("inconsistent after pool recycling:\n  first = %s\n  third = %s", dst1, dst3)
	}
}

func BenchmarkSign(b *testing.B) {
	secret := []byte("bench-secret")
	InitHMACPool(secret)

	payload := []byte(`{"event":"transaction","account_id":"0:abcdef","tx_hash":"0123456789abcdef","lt":100,"timestamp":1700000000}`)
	dst := make([]byte, 64)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		Sign(dst, payload)
	}
}

// ---------------------------------------------------------------------------
// Integration test
// ---------------------------------------------------------------------------

func TestBuildAndSign_Integration(t *testing.T) {
	secret := []byte("integration-secret")
	InitHMACPool(secret)

	tx := domain.Transaction{
		AccountID:  "0:integration-test-addr",
		TxHash:     "aabbccdd",
		Sender:     "EQBintegration-sender",
		SenderName: "IntegBot",
		Recipient:  "EQBintegration-recipient",
		Amount:     2_000_000_000,
		ActionType: "Ton Transfer",
		Value:      "2 TON",
		Comment:    "integration test",
		Lt:         12345,
		Timestamp:  1700000000,
	}

	// Step 1: build payload using pooled buffer.
	bp := acquireBuffer()
	defer releaseBuffer(bp)
	*bp = BuildPayload(*bp, tx)

	// Verify JSON validity.
	var m map[string]any
	if err := json.Unmarshal(*bp, &m); err != nil {
		t.Fatalf("invalid JSON from builder: %v", err)
	}

	// Step 2: sign the payload.
	hexBuf := acquireHex()
	defer releaseHex(hexBuf)
	n := Sign(*hexBuf, *bp)

	if n != 64 {
		t.Fatalf("Sign returned %d, want 64", n)
	}

	sig := string((*hexBuf)[:n])

	// Verify signature independently.
	mac := hmac.New(sha256.New, secret)
	mac.Write(*bp)
	expected := hex.EncodeToString(mac.Sum(nil))

	if sig != expected {
		t.Errorf("integration signature mismatch:\n  got  = %s\n  want = %s", sig, expected)
	}

	// Step 3: verify zero allocs for the build step with testing.AllocsPerRun.
	allocs := testing.AllocsPerRun(100, func() {
		*bp = (*bp)[:0]
		*bp = BuildPayload(*bp, tx)
	})
	if allocs != 0 {
		t.Errorf("BuildPayload allocs = %.0f, want 0", allocs)
	}
}
