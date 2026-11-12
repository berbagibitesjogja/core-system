package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
)

const DonationAutoCompleteLockID int64 = 7483920192

type DonationAutoCompleteJob struct {
	pool              *pgxpool.Pool
	queries           *dbgen.Queries
	autoCompleteAfter time.Duration
	interval          time.Duration
	now               func() time.Time
	logger            *slog.Logger
}

func NewDonationAutoCompleteJob(
	pool *pgxpool.Pool,
	queries *dbgen.Queries,
	cfg config.DonationConfig,
	logger *slog.Logger,
) *DonationAutoCompleteJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &DonationAutoCompleteJob{
		pool:              pool,
		queries:           queries,
		autoCompleteAfter: cfg.AutoCompleteAfter,
		interval:          cfg.AutoCompleteInterval,
		now:               time.Now,
		logger:            logger,
	}
}

func (j *DonationAutoCompleteJob) SetNow(fn func() time.Time) {
	if fn != nil {
		j.now = fn
	}
}

func (j *DonationAutoCompleteJob) RunOnce(ctx context.Context) ([]uuid.UUID, error) {
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := j.queries.WithTx(tx)

	locked, err := qtx.TryAdvisoryXactLock(ctx, DonationAutoCompleteLockID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire advisory xact lock: %w", err)
	}
	if !locked {
		j.logger.DebugContext(ctx, "donation autocomplete advisory lock not acquired, skipping iteration")
		return nil, nil
	}

	cutoff := j.now().Add(-j.autoCompleteAfter)

	completedIDs, err := qtx.AutoCompleteDonations(ctx, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to auto complete donations: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit auto complete transaction: %w", err)
	}

	if len(completedIDs) > 0 {
		j.logger.InfoContext(ctx, "donation auto-complete completed donations",
			"count", len(completedIDs),
			"cutoff", cutoff.Format(time.RFC3339),
		)
	}

	return completedIDs, nil
}

func (j *DonationAutoCompleteJob) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	j.logger.Info("donation auto-complete background worker started", "interval", j.interval.String())

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("donation auto-complete background worker stopped")
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						j.logger.Error("donation auto-complete iteration panicked", "panic", r)
					}
				}()
				if _, err := j.RunOnce(ctx); err != nil {
					if !errors.Is(err, context.Canceled) && ctx.Err() == nil {
						j.logger.Error("donation auto-complete iteration failed", "error", err)
					}
				}
			}()
		}
	}
}
