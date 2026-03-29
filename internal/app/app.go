package app

import (
	"context"
	"time"

	"ton-monitoring/internal/config"
	"ton-monitoring/internal/cursor"
	"ton-monitoring/internal/dedup"
	"ton-monitoring/internal/domain"
	"ton-monitoring/internal/health"
	"ton-monitoring/internal/stream"
	"ton-monitoring/internal/webhook"

	"github.com/rs/zerolog/log"
)

type App struct {
	cfg      config.Config
	listener *stream.Listener
	sender   *webhook.Sender
	cursors  *cursor.Store
	dedup    dedup.Ring
	health   *health.Server
}

func New(cfg config.Config) (*App, error) {
	webhook.InitHMACPool(cfg.WebhookSecret)

	cs, err := cursor.Open(cfg.BoltDBPath)
	if err != nil {
		return nil, err
	}

	listener, err := stream.NewListener(cfg.TonAPIToken, cfg.WatchAddresses, cfg.Testnet, cfg.GapFillLimit)
	if err != nil {
		if cerr := cs.Close(); cerr != nil {
			log.Error().Err(cerr).Msg("cursor close failed")
		}
		return nil, err
	}

	sender := webhook.NewSender(webhook.SenderConfig{
		URL:         cfg.WebhookURL,
		Timeout:     cfg.WebhookTimeout,
		MaxAttempts: cfg.RetryMaxAttempts,
		BaseDelay:   cfg.RetryBaseDelay,
		MaxDelay:    cfg.RetryMaxDelay,
	})

	hs := health.NewServer(cfg.HealthAddr, listener.Connected)

	sender.OnDelivered = func(tx domain.Transaction) {
		if err := cs.Save(domain.Cursor{
			AccountID: tx.AccountID,
			TxHash:    tx.TxHash,
			Lt:        tx.Lt,
		}); err != nil {
			log.Error().Err(err).Str("account", tx.AccountID).Msg("cursor save failed")
		}
	}

	return &App{
		cfg:      cfg,
		listener: listener,
		sender:   sender,
		cursors:  cs,
		health:   hs,
	}, nil
}

func (a *App) ProcessTransaction(tx domain.Transaction) {
	if a.dedup.ContainsOrInsert(tx.Lt, tx.TxHash) {
		return
	}

	result := a.sender.Send(tx)

	if result.Retry {
		a.sender.EnqueueRetry(tx, 0)
		return
	}

	if result.StatusCode >= 200 && result.StatusCode < 300 {
		err := a.cursors.Save(domain.Cursor{
			AccountID: tx.AccountID,
			TxHash:    tx.TxHash,
			Lt:        tx.Lt,
		})
		if err != nil {
			log.Error().Err(err).Str("account", tx.AccountID).Msg("cursor save failed")
		}
	}
}

func (a *App) Run(ctx context.Context) error {
	go func() {
		if err := a.health.ListenAndServe(); err != nil && ctx.Err() == nil {
			log.Error().Err(err).Msg("health server error")
		}
	}()

	retryCtx, retryCancel := context.WithCancel(ctx)
	defer retryCancel()
	go a.sender.RunRetryWorker(retryCtx)

	if a.cfg.GapFillEnabled {
		cursors, err := a.cursors.LoadAll()
		if err != nil {
			log.Warn().Err(err).Msg("failed to load cursors for gap-fill")
		} else {
			cursorMap := make(map[string]domain.Cursor, len(cursors))
			for _, c := range cursors {
				cursorMap[c.AccountID] = c
			}
			a.listener.GapFill(ctx, cursorMap, a.ProcessTransaction)
		}
	}

	return a.listener.Run(ctx, a.ProcessTransaction)
}

func (a *App) Shutdown() {
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()
	a.sender.DrainRetries(drainCtx)

	if err := a.cursors.Close(); err != nil {
		log.Error().Err(err).Msg("cursor close failed")
	}

	healthCtx, healthCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer healthCancel()
	if err := a.health.Shutdown(healthCtx); err != nil {
		log.Error().Err(err).Msg("health server shutdown failed")
	}
	healthCancel()

	log.Info().Msg("shutdown complete")
}
