package stream

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ton-monitoring/internal/domain"

	"github.com/rs/zerolog/log"
	"github.com/tonkeeper/tonapi-go"
)

type Listener struct {
	streaming    *tonapi.StreamingAPI
	client       *tonapi.Client
	accounts     []string
	gapFillLimit int
	connected    atomic.Bool
}

func NewListener(token string, accounts []string, testnet bool, gapFillLimit int) (*Listener, error) {
	var opts []tonapi.StreamingOption
	if token != "" {
		opts = append(opts, tonapi.WithStreamingToken(token))
	}

	var clientURL string
	if testnet {
		clientURL = tonapi.TestnetTonApiURL
		opts = append(opts, tonapi.WithStreamingTestnet())
	} else {
		clientURL = tonapi.TonApiURL
	}

	var sec tonapi.SecuritySource = &tonapi.Security{}
	if token != "" {
		sec = tonapi.WithToken(token)
	}
	client, err := tonapi.NewClient(clientURL, sec)
	if err != nil {
		return nil, err
	}

	return &Listener{
		streaming:    tonapi.NewStreamingAPI(opts...),
		client:       client,
		accounts:     accounts,
		gapFillLimit: gapFillLimit,
	}, nil
}

func (l *Listener) Connected() bool {
	return l.connected.Load()
}

func (l *Listener) GapFill(ctx context.Context, cursors map[string]domain.Cursor, handler func(domain.Transaction)) {
	for _, addr := range l.accounts {
		cur := cursors[addr]

		limit := int32(l.gapFillLimit)
		if limit <= 0 {
			limit = 5
		}

		params := tonapi.GetBlockchainAccountTransactionsParams{
			AccountID: addr,
			Limit:     tonapi.NewOptInt32(limit),
		}

		resp, err := l.client.GetBlockchainAccountTransactions(ctx, params)
		if err != nil {
			log.Warn().Err(err).Str("account", addr).Msg("gap-fill fetch failed")
			continue
		}

		var missed []domain.Transaction
		for _, tx := range resp.Transactions {
			lt := uint64(tx.Lt)
			if cur.Lt > 0 && lt <= cur.Lt {
				break
			}
			missed = append(missed, domain.Transaction{
				AccountID: addr,
				TxHash:    tx.Hash,
				Lt:        lt,
				Timestamp: tx.Utime,
			})
		}

		for i := len(missed) - 1; i >= 0; i-- {
			if l.enrichTransaction(ctx, &missed[i]) {
				handler(missed[i])
			}
		}

		if len(missed) > 0 {
			log.Info().Str("account", addr).Int("count", len(missed)).Msg("gap-fill processed")
		}
	}
}

func (l *Listener) Run(ctx context.Context, handler func(domain.Transaction)) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		var once sync.Once

		log.Info().Strs("accounts", l.accounts).Msg("SSE subscribing")

		err := l.streaming.SubscribeToTransactions(ctx, l.accounts, nil,
			func(data tonapi.TransactionEventData) {
				once.Do(func() {
					l.connected.Store(true)
					backoff = time.Second
					log.Info().Msg("SSE first event received, connected")
				})

				log.Debug().
					Str("account", data.AccountID.String()).
					Str("hash", data.TxHash).
					Uint64("lt", data.Lt).
					Msg("SSE event received")

				tx := domain.Transaction{
					AccountID: data.AccountID.String(),
					TxHash:    data.TxHash,
					Lt:        data.Lt,
					Timestamp: time.Now().Unix(),
				}

				if l.enrichTransaction(ctx, &tx) {
					log.Debug().
						Str("action", tx.ActionType).
						Str("value", tx.Value).
						Str("sender", tx.Sender).
						Msg("sending webhook")
					handler(tx)
				} else {
					log.Debug().
						Str("hash", tx.TxHash).
						Str("value", tx.Value).
						Int64("amount", tx.Amount).
						Msg("event filtered out")
				}
			})

		l.connected.Store(false)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Warn().Err(err).Dur("backoff", backoff).Msg("SSE disconnected, reconnecting")

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

const minAmountNano = 500_000_000

func (l *Listener) enrichTransaction(ctx context.Context, tx *domain.Transaction) bool {
	event, err := l.client.GetAccountEvent(ctx, tonapi.GetAccountEventParams{
		AccountID: tx.AccountID,
		EventID:   tx.TxHash,
	})
	if err != nil {
		log.Debug().Err(err).Str("tx", tx.TxHash).Msg("enrich failed")
		return false
	}

	if event.InProgress {
		return false
	}

	if len(event.Actions) == 0 {
		return false
	}
	action := event.Actions[0]

	tx.ActionType = action.SimplePreview.Name
	if v, ok := action.SimplePreview.Value.Get(); ok {
		tx.Value = v
	}
	if len(action.SimplePreview.Accounts) > 0 {
		acc := action.SimplePreview.Accounts[0]
		tx.Sender = acc.Address
		if name, ok := acc.Name.Get(); ok {
			tx.SenderName = name
		}
	}

	if transfer, ok := action.TonTransfer.Get(); ok {
		tx.Sender = transfer.Sender.Address
		if name, ok := transfer.Sender.Name.Get(); ok {
			tx.SenderName = name
		}
		tx.Recipient = transfer.Recipient.Address
		tx.Amount = transfer.Amount
		if comment, ok := transfer.Comment.Get(); ok {
			tx.Comment = comment
		}
		return tx.Amount >= minAmountNano
	}

	if transfer, ok := action.JettonTransfer.Get(); ok {
		if s, ok := transfer.Sender.Get(); ok {
			tx.Sender = s.Address
			if name, ok := s.Name.Get(); ok {
				tx.SenderName = name
			}
		}
		if r, ok := transfer.Recipient.Get(); ok {
			tx.Recipient = r.Address
		}
		if comment, ok := transfer.Comment.Get(); ok {
			tx.Comment = comment
		}
		return parseValueAmount(tx.Value) >= 0.5
	}

	return true
}

func parseValueAmount(v string) float64 {
	end := 0
	for end < len(v) && (v[end] >= '0' && v[end] <= '9' || v[end] == '.' || v[end] == '-') {
		end++
	}
	if end == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(v[:end], 64)
	return f
}
