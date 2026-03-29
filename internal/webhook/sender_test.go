package webhook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ton-monitoring/internal/domain"
)

// testTx returns a deterministic transaction for test use.
func testTx() domain.Transaction {
	return domain.Transaction{
		AccountID: "0:abc123",
		TxHash:    "deadbeef",
		Lt:        42,
		Timestamp: 1700000000,
	}
}

// newTestSender creates a Sender pointed at the given URL with short timeouts.
func newTestSender(url string) *Sender {
	return NewSender(SenderConfig{
		URL:         url,
		Timeout:     5 * time.Second,
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    time.Second,
	})
}

// ---------------------------------------------------------------------------
// Send tests
// ---------------------------------------------------------------------------

func TestSend_Success(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	var gotContentType string
	var gotSignature string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotSignature = r.Header.Get("X-Webhook-Signature")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := newTestSender(srv.URL)
	tx := testTx()
	result := sender.Send(tx)

	// Verify status code.
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	// Must not be marked for retry.
	if result.Retry {
		t.Error("Retry = true, want false on 200")
	}

	// Verify Content-Type header.
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}

	// Verify signature header is 64 hex characters.
	if len(gotSignature) != 64 {
		t.Errorf("X-Webhook-Signature length = %d, want 64", len(gotSignature))
	}
	// Signature must be lowercase hex.
	for _, c := range gotSignature {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("X-Webhook-Signature contains non-hex char: %c", c)
			break
		}
	}

	// Verify body is valid JSON with expected fields.
	var m map[string]any
	if err := json.Unmarshal(gotBody, &m); err != nil {
		t.Fatalf("invalid JSON body: %v\nbody: %s", err, gotBody)
	}
	if got := m["event"]; got != "transaction" {
		t.Errorf("event = %v, want transaction", got)
	}
	if got := m["account_id"]; got != "0:abc123" {
		t.Errorf("account_id = %v, want 0:abc123", got)
	}
	if got := m["tx_hash"]; got != "deadbeef" {
		t.Errorf("tx_hash = %v, want deadbeef", got)
	}
	if got, ok := m["lt"].(float64); !ok || got != 42 {
		t.Errorf("lt = %v, want 42", m["lt"])
	}
	if got, ok := m["timestamp"].(float64); !ok || got != 1700000000 {
		t.Errorf("timestamp = %v, want 1700000000", m["timestamp"])
	}
}

func TestSend_ServerError(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sender := newTestSender(srv.URL)
	result := sender.Send(testTx())

	if result.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", result.StatusCode)
	}
	if !result.Retry {
		t.Error("Retry = false, want true on 500")
	}
}

func TestSend_ClientError(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	sender := newTestSender(srv.URL)
	result := sender.Send(testTx())

	if result.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", result.StatusCode)
	}
	if result.Retry {
		t.Error("Retry = true, want false on 400")
	}
}

func TestSend_RateLimited(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	sender := newTestSender(srv.URL)
	now := time.Now().Unix()
	result := sender.Send(testTx())

	if result.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", result.StatusCode)
	}
	if !result.Retry {
		t.Error("Retry = false, want true on 429")
	}
	// RetryAfter should be approximately now+30 (within 2 seconds tolerance).
	expectedMin := now + 28
	expectedMax := now + 32
	if result.RetryAfter < expectedMin || result.RetryAfter > expectedMax {
		t.Errorf("RetryAfter = %d, want ~%d (range %d-%d)",
			result.RetryAfter, now+30, expectedMin, expectedMax)
	}
}

func TestSend_NetworkError(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	// Point at a closed server to trigger a network error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()

	sender := newTestSender(url)
	result := sender.Send(testTx())

	// Network error should trigger retry.
	if !result.Retry {
		t.Error("Retry = false, want true on network error")
	}
	// StatusCode should be zero (no response received).
	if result.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0 on network error", result.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// bodyReader tests
// ---------------------------------------------------------------------------

func TestBodyReader_Read(t *testing.T) {
	data := []byte("hello, world")
	r := &bodyReader{data: data}

	buf := make([]byte, 5)

	// First read: "hello"
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error on first read: %v", err)
	}
	if n != 5 || string(buf[:n]) != "hello" {
		t.Errorf("first read = %q, want %q", buf[:n], "hello")
	}

	// Second read: ", wor"
	n, err = r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error on second read: %v", err)
	}
	if n != 5 || string(buf[:n]) != ", wor" {
		t.Errorf("second read = %q, want %q", buf[:n], ", wor")
	}

	// Third read: "ld"
	n, err = r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error on third read: %v", err)
	}
	if n != 2 || string(buf[:n]) != "ld" {
		t.Errorf("third read = %q, want %q", buf[:n], "ld")
	}

	// Fourth read: EOF
	n, err = r.Read(buf)
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
	if n != 0 {
		t.Errorf("read after EOF returned %d bytes", n)
	}
}

func TestBodyReader_Close(t *testing.T) {
	r := &bodyReader{data: []byte("test")}
	if err := r.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestBodyReader_ImplementsReadCloser(t *testing.T) {
	// Compile-time verification that bodyReader satisfies io.ReadCloser.
	var _ io.ReadCloser = (*bodyReader)(nil)
}

// ---------------------------------------------------------------------------
// RetryQueue tests
// ---------------------------------------------------------------------------

func TestRetryQueue_PushPop(t *testing.T) {
	q := newRetryQueue(256)

	items := []RetryItem{
		{Tx: domain.Transaction{AccountID: "a1", Lt: 1}, Attempt: 1, NextAt: 100},
		{Tx: domain.Transaction{AccountID: "a2", Lt: 2}, Attempt: 2, NextAt: 200},
		{Tx: domain.Transaction{AccountID: "a3", Lt: 3}, Attempt: 3, NextAt: 300},
	}

	// Push all items.
	for i, item := range items {
		if !q.Push(item) {
			t.Fatalf("Push(%d) returned false, want true", i)
		}
	}

	if q.Len() != 3 {
		t.Errorf("Len() = %d, want 3", q.Len())
	}

	// Pop in FIFO order.
	for i, want := range items {
		got, ok := q.Pop()
		if !ok {
			t.Fatalf("Pop(%d) returned false, want true", i)
		}
		if got.Tx.AccountID != want.Tx.AccountID {
			t.Errorf("Pop(%d).Tx.AccountID = %q, want %q", i, got.Tx.AccountID, want.Tx.AccountID)
		}
		if got.Tx.Lt != want.Tx.Lt {
			t.Errorf("Pop(%d).Tx.Lt = %d, want %d", i, got.Tx.Lt, want.Tx.Lt)
		}
		if got.Attempt != want.Attempt {
			t.Errorf("Pop(%d).Attempt = %d, want %d", i, got.Attempt, want.Attempt)
		}
		if got.NextAt != want.NextAt {
			t.Errorf("Pop(%d).NextAt = %d, want %d", i, got.NextAt, want.NextAt)
		}
	}

	if q.Len() != 0 {
		t.Errorf("Len() after drain = %d, want 0", q.Len())
	}
}

func TestRetryQueue_Full(t *testing.T) {
	const queueCap = 256
	q := newRetryQueue(queueCap)

	// Fill queue to capacity.
	for i := range queueCap {
		item := RetryItem{
			Tx:      domain.Transaction{AccountID: "fill", Lt: uint64(i)},
			Attempt: 1,
			NextAt:  int64(i),
		}
		if !q.Push(item) {
			t.Fatalf("Push(%d) returned false before queue full", i)
		}
	}

	if q.Len() != queueCap {
		t.Errorf("Len() = %d, want %d", q.Len(), queueCap)
	}

	// Next push must fail.
	overflow := RetryItem{
		Tx:      domain.Transaction{AccountID: "overflow", Lt: 999},
		Attempt: 1,
		NextAt:  999,
	}
	if q.Push(overflow) {
		t.Error("Push to full queue returned true, want false")
	}

	// Pop one and verify we can push again.
	got, ok := q.Pop()
	if !ok {
		t.Fatal("Pop from full queue returned false")
	}
	if got.Tx.Lt != 0 {
		t.Errorf("first popped Lt = %d, want 0", got.Tx.Lt)
	}

	if !q.Push(overflow) {
		t.Error("Push after Pop returned false, want true")
	}
}

func TestRetryQueue_Empty(t *testing.T) {
	q := newRetryQueue(256)

	_, ok := q.Pop()
	if ok {
		t.Error("Pop from empty queue returned true, want false")
	}

	if q.Len() != 0 {
		t.Errorf("Len() of empty queue = %d, want 0", q.Len())
	}
}

func TestRetryQueue_Wraparound(t *testing.T) {
	const queueCap = 256
	q := newRetryQueue(queueCap)

	// Push and pop enough to exercise the queue through multiple fill/drain cycles.
	for cycle := range 3 {
		for i := range queueCap {
			item := RetryItem{
				Tx:      domain.Transaction{Lt: uint64(cycle*queueCap + i)},
				Attempt: 1,
			}
			if !q.Push(item) {
				t.Fatalf("cycle %d, push %d: returned false", cycle, i)
			}
		}
		for i := range queueCap {
			got, ok := q.Pop()
			if !ok {
				t.Fatalf("cycle %d, pop %d: returned false", cycle, i)
			}
			wantLt := uint64(cycle*queueCap + i)
			if got.Tx.Lt != wantLt {
				t.Errorf("cycle %d, pop %d: Lt = %d, want %d", cycle, i, got.Tx.Lt, wantLt)
			}
		}
	}

	if q.Len() != 0 {
		t.Errorf("Len() after wraparound = %d, want 0", q.Len())
	}
}

// ---------------------------------------------------------------------------
// NewSender configuration test
// ---------------------------------------------------------------------------

func TestNewSender_Defaults(t *testing.T) {
	cfg := SenderConfig{
		URL:         "http://example.com/hook",
		Timeout:     10 * time.Second,
		MaxAttempts: 5,
		BaseDelay:   time.Second,
		MaxDelay:    30 * time.Second,
	}

	s := NewSender(cfg)

	if s.url != cfg.URL {
		t.Errorf("url = %q, want %q", s.url, cfg.URL)
	}
	if s.maxAttempts != 5 {
		t.Errorf("maxAttempts = %d, want 5", s.maxAttempts)
	}
	if s.baseDelay != time.Second {
		t.Errorf("baseDelay = %v, want %v", s.baseDelay, time.Second)
	}
	if s.maxDelay != 30*time.Second {
		t.Errorf("maxDelay = %v, want %v", s.maxDelay, 30*time.Second)
	}
	if s.client == nil {
		t.Fatal("client is nil")
	}
	if s.client.Timeout != 10*time.Second {
		t.Errorf("client.Timeout = %v, want %v", s.client.Timeout, 10*time.Second)
	}
}

// ---------------------------------------------------------------------------
// EnqueueRetry test
// ---------------------------------------------------------------------------

func TestEnqueueRetry_ExponentialBackoff(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	sender := NewSender(SenderConfig{
		URL:         "http://localhost:0",
		Timeout:     time.Second,
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
	})

	tx := testTx()

	// Enqueue at attempt 0 -- should succeed.
	sender.EnqueueRetry(tx, 0)
	if sender.retry.Len() != 1 {
		t.Fatalf("retry queue len = %d, want 1", sender.retry.Len())
	}

	item, ok := sender.retry.Pop()
	if !ok {
		t.Fatal("Pop returned false after enqueue")
	}
	if item.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", item.Attempt)
	}
	if item.Tx.AccountID != tx.AccountID {
		t.Errorf("AccountID = %q, want %q", item.Tx.AccountID, tx.AccountID)
	}
}

func TestEnqueueRetry_ExhaustedRetries(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	sender := NewSender(SenderConfig{
		URL:         "http://localhost:0",
		Timeout:     time.Second,
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
	})

	tx := testTx()

	// Enqueue at maxAttempts -- should be dropped.
	sender.EnqueueRetry(tx, 3)
	if sender.retry.Len() != 0 {
		t.Errorf("retry queue len = %d, want 0 (should be dropped)", sender.retry.Len())
	}
}

// ---------------------------------------------------------------------------
// Integration: Send round-trips through httptest
// ---------------------------------------------------------------------------

func TestSend_SignatureMatchesBody(t *testing.T) {
	secret := []byte("sig-verify-secret")
	InitHMACPool(secret)

	var capturedBody []byte
	var capturedSig string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSig = r.Header.Get("X-Webhook-Signature")
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := newTestSender(srv.URL)
	sender.Send(testTx())

	// Re-compute signature from captured body and verify it matches.
	dst := make([]byte, 64)
	Sign(dst, capturedBody)
	expectedSig := string(dst)

	if capturedSig != expectedSig {
		t.Errorf("signature mismatch:\n  got  = %s\n  want = %s", capturedSig, expectedSig)
	}
}

func TestSend_ContentLengthSet(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	var gotContentLength int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentLength = r.ContentLength
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := newTestSender(srv.URL)
	sender.Send(testTx())

	if gotContentLength <= 0 {
		t.Errorf("ContentLength = %d, want > 0", gotContentLength)
	}

	// Build the expected payload to verify exact content length.
	buf := BuildPayload(make([]byte, 0, 512), testTx())
	if gotContentLength != int64(len(buf)) {
		t.Errorf("ContentLength = %d, want %d", gotContentLength, len(buf))
	}
}

func TestSend_MethodIsPost(t *testing.T) {
	InitHMACPool([]byte("test-secret"))

	var gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := newTestSender(srv.URL)
	sender.Send(testTx())

	if gotMethod != http.MethodPost {
		t.Errorf("Method = %q, want %q", gotMethod, http.MethodPost)
	}
}
