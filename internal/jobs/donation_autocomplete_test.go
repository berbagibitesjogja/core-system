package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/database"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	cfg := config.LoadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, &cfg.DBConfig)
	if err != nil {
		t.Skipf("skipping test: database not reachable: %v", err)
	}
	return pool
}

func TestDonationAutoCompleteJob_RunOnce(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	queries := dbgen.New(pool)
	cfg := config.LoadConfig()
	cfg.Donation.AutoCompleteAfter = 2 * time.Hour

	// 1. Create a sponsor
	var sponsorID uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO sponsors (name, email, phone, variant)
		VALUES ('Auto-Complete Test Sponsor', 'auto@test.com', '081234567890', 'individual')
		RETURNING id;
	`).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to create sponsor: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE sponsor_id = $1", sponsorID)
		_, _ = pool.Exec(ctx, "DELETE FROM sponsors WHERE id = $1", sponsorID)
	}()

	now := time.Now()

	// Helper to insert donation
	insertDonation := func(status string, eventAt time.Time) uuid.UUID {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO donations (
				sponsor_id, event_at, quota, hero_quota, remain,
				address, message, status, hide_from_public, hide_sponsor
			) VALUES (
				$1, $2, 50, 20, 20,
				'Test Address', 'Test Message', $3::donation_status, FALSE, FALSE
			) RETURNING id;
		`, sponsorID, eventAt, status).Scan(&id)
		if err != nil {
			t.Fatalf("failed to insert donation (%s, %v): %v", status, eventAt, err)
		}
		return id
	}

	// a) open donation 3h ago (eligible)
	idOpenPast := insertDonation("open", now.Add(-3*time.Hour))
	// b) closed donation 3h ago (eligible)
	idClosedPast := insertDonation("closed", now.Add(-3*time.Hour))
	// c) draft donation 3h ago (should NOT be touched)
	idDraftPast := insertDonation("draft", now.Add(-3*time.Hour))
	// d) open donation 30m ago (grace window is 2h -> should NOT be touched)
	idOpenRecent := insertDonation("open", now.Add(-30*time.Minute))
	// e) open donation 2h in future (should NOT be touched)
	idOpenFuture := insertDonation("open", now.Add(2*time.Hour))

	job := NewDonationAutoCompleteJob(pool, queries, cfg.Donation, nil)
	job.SetNow(func() time.Time { return now })

	completedIDs, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("job.RunOnce failed: %v", err)
	}

	// Verify returned IDs contain idOpenPast and idClosedPast
	completedMap := make(map[uuid.UUID]bool)
	for _, id := range completedIDs {
		completedMap[id] = true
	}

	if !completedMap[idOpenPast] {
		t.Errorf("expected open past donation %s to be auto-completed", idOpenPast)
	}
	if !completedMap[idClosedPast] {
		t.Errorf("expected closed past donation %s to be auto-completed", idClosedPast)
	}
	if completedMap[idDraftPast] {
		t.Errorf("draft donation %s must not be auto-completed", idDraftPast)
	}
	if completedMap[idOpenRecent] {
		t.Errorf("recent donation %s within grace period must not be auto-completed", idOpenRecent)
	}
	if completedMap[idOpenFuture] {
		t.Errorf("future donation %s must not be auto-completed", idOpenFuture)
	}

	// Verify database statuses
	checkStatus := func(id uuid.UUID, expected string) {
		var status string
		err := pool.QueryRow(ctx, "SELECT status FROM donations WHERE id = $1", id).Scan(&status)
		if err != nil {
			t.Fatalf("failed to query donation status for %s: %v", id, err)
		}
		if status != expected {
			t.Errorf("donation %s: expected status %s, got %s", id, expected, status)
		}
	}

	checkStatus(idOpenPast, "completed")
	checkStatus(idClosedPast, "completed")
	checkStatus(idDraftPast, "draft")
	checkStatus(idOpenRecent, "open")
	checkStatus(idOpenFuture, "open")

	// Test advancing mock clock so idOpenRecent becomes eligible
	job.SetNow(func() time.Time { return now.Add(3 * time.Hour) })
	completedIDs2, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second job.RunOnce failed: %v", err)
	}

	completedMap2 := make(map[uuid.UUID]bool)
	for _, id := range completedIDs2 {
		completedMap2[id] = true
	}

	if !completedMap2[idOpenRecent] {
		t.Errorf("expected recent donation %s to be auto-completed after mock time advance", idOpenRecent)
	}
	checkStatus(idOpenRecent, "completed")
}

func TestDonationAutoCompleteJob_AdvisoryLockSkipping(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	queries := dbgen.New(pool)
	cfg := config.LoadConfig()

	job := NewDonationAutoCompleteJob(pool, queries, cfg.Donation, nil)

	// Open a tx in a separate connection that holds the advisory lock
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin holding tx: %v", err)
	}
	defer tx.Rollback(ctx)

	var lockAcquired bool
	err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", DonationAutoCompleteLockID).Scan(&lockAcquired)
	if err != nil {
		t.Fatalf("failed to acquire holding lock: %v", err)
	}
	if !lockAcquired {
		t.Fatalf("expected to acquire holding lock")
	}

	// Now run job.RunOnce from another connection (from the pool)
	// It should gracefully detect !locked, return nil error and 0 items without blocking
	completedIDs, err := job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("job.RunOnce failed when lock held: %v", err)
	}
	if len(completedIDs) > 0 {
		t.Errorf("expected 0 completed IDs when lock held by peer, got %d", len(completedIDs))
	}

	// Release lock by rolling back holding tx
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("failed to rollback holding tx: %v", err)
	}

	// Verify next job.RunOnce succeeds and can acquire the lock
	completedIDs, err = job.RunOnce(ctx)
	if err != nil {
		t.Fatalf("job.RunOnce failed after peer lock released: %v", err)
	}
	_ = completedIDs
}

func TestDonationAutoCompleteJob_StartCancellation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	queries := dbgen.New(pool)
	cfg := config.LoadConfig()
	cfg.Donation.AutoCompleteInterval = 10 * time.Millisecond

	job := NewDonationAutoCompleteJob(pool, queries, cfg.Donation, nil)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	wg.Add(1)
	go job.Start(ctx, &wg)

	// Let it run for a brief moment then cancel context
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Wait for goroutine to exit gracefully
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Passed gracefully
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for job.Start to stop on context cancellation")
	}
}
