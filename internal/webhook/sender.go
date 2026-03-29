package webhook

import (
	"context"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"ton-monitoring/internal/domain"

	"github.com/rs/zerolog/log"
)

type Sender struct {
	client      *http.Client
	url         string
	retry       *RetryQueue
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	OnDelivered func(domain.Transaction)
}

type SenderConfig struct {
	URL         string
	Timeout     time.Duration
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func NewSender(cfg SenderConfig) *Sender {
	return &Sender{
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  true,
				ForceAttemptHTTP2:   true,
				WriteBufferSize:     4096,
				ReadBufferSize:      4096,
			},
		},
		url:         cfg.URL,
		retry:       newRetryQueue(256),
		maxAttempts: cfg.MaxAttempts,
		baseDelay:   cfg.BaseDelay,
		maxDelay:    cfg.MaxDelay,
	}
}

func (s *Sender) Send(tx domain.Transaction) domain.DeliveryResult {
	bp := acquireBuffer()
	buf := BuildPayload(*bp, tx)

	hexBuf := acquireHex()
	Sign(*hexBuf, buf)

	body := &bodyReader{data: buf}
	req, err := http.NewRequest(http.MethodPost, s.url, body)
	if err != nil {
		releaseBuffer(bp)
		releaseHex(hexBuf)
		return domain.DeliveryResult{Retry: false}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", string((*hexBuf)[:64]))
	req.ContentLength = int64(len(buf))

	resp, err := s.client.Do(req)
	if err != nil {
		*bp = buf[:0]
		releaseBuffer(bp)
		releaseHex(hexBuf)
		log.Warn().Err(err).Str("url", s.url).Msg("webhook delivery failed")
		return domain.DeliveryResult{Retry: true}
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		log.Warn().Err(err).Msg("webhook response body close failed")
	}

	*bp = buf[:0]
	releaseBuffer(bp)
	releaseHex(hexBuf)

	result := domain.DeliveryResult{StatusCode: resp.StatusCode}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
	case resp.StatusCode == 429:
		result.Retry = true
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.ParseInt(ra, 10, 64); err == nil {
				result.RetryAfter = time.Now().Unix() + secs
			}
		}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		log.Warn().Int("status", resp.StatusCode).Msg("webhook permanent failure")
	default:
		result.Retry = true
		log.Warn().Int("status", resp.StatusCode).Msg("webhook transient failure")
	}

	return result
}

type bodyReader struct {
	data []byte
	pos  int
}

func (r *bodyReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *bodyReader) Close() error { return nil }

type RetryItem struct {
	Tx      domain.Transaction
	Attempt int
	NextAt  int64
}

type RetryQueue struct {
	mu    sync.Mutex
	items []RetryItem
	cap   int
}

func newRetryQueue(cap int) *RetryQueue {
	return &RetryQueue{cap: cap}
}

func (q *RetryQueue) Push(item RetryItem) bool {
	q.mu.Lock()
	if len(q.items) >= q.cap {
		q.mu.Unlock()
		return false
	}
	q.items = append(q.items, item)
	q.mu.Unlock()
	return true
}

func (q *RetryQueue) Pop() (RetryItem, bool) {
	q.mu.Lock()
	if len(q.items) == 0 {
		q.mu.Unlock()
		return RetryItem{}, false
	}
	item := q.items[0]
	q.items[0] = RetryItem{}
	q.items = q.items[1:]
	q.mu.Unlock()
	return item, true
}

func (q *RetryQueue) Peek() (RetryItem, bool) {
	q.mu.Lock()
	if len(q.items) == 0 {
		q.mu.Unlock()
		return RetryItem{}, false
	}
	item := q.items[0]
	q.mu.Unlock()
	return item, true
}

func (q *RetryQueue) Len() int {
	q.mu.Lock()
	n := len(q.items)
	q.mu.Unlock()
	return n
}

func (s *Sender) EnqueueRetry(tx domain.Transaction, attempt int) {
	if attempt >= s.maxAttempts {
		log.Error().
			Str("account", tx.AccountID).
			Uint64("lt", tx.Lt).
			Int("attempts", attempt).
			Msg("webhook delivery exhausted retries")
		return
	}

	delay := float64(s.baseDelay) * math.Pow(2, float64(attempt))
	if delay > float64(s.maxDelay) {
		delay = float64(s.maxDelay)
	}

	item := RetryItem{
		Tx:      tx,
		Attempt: attempt + 1,
		NextAt:  time.Now().Add(time.Duration(delay)).Unix(),
	}

	if !s.retry.Push(item) {
		log.Error().Msg("retry queue full, dropping webhook")
	}
}

func (s *Sender) ProcessRetries() {
	now := time.Now().Unix()
	for {
		item, ok := s.retry.Peek()
		if !ok {
			return
		}
		if item.NextAt > now {
			return
		}
		s.retry.Pop()
		result := s.Send(item.Tx)
		if result.Retry {
			s.EnqueueRetry(item.Tx, item.Attempt)
		} else if result.StatusCode >= 200 && result.StatusCode < 300 && s.OnDelivered != nil {
			s.OnDelivered(item.Tx)
		}
	}
}

func (s *Sender) RunRetryWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ProcessRetries()
		}
	}
}

func (s *Sender) DrainRetries(ctx context.Context) {
	for {
		item, ok := s.retry.Pop()
		if !ok {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		result := s.Send(item.Tx)
		if result.Retry {
			log.Warn().
				Str("account", item.Tx.AccountID).
				Uint64("lt", item.Tx.Lt).
				Msg("retry failed during shutdown drain")
		}
	}
}
