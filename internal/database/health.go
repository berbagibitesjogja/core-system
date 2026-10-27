package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DBHealth struct {
	Status        string
	Latency       time.Duration
	TotalConns    int32
	IdleConns     int32
	AcquiredConns int32
	Error         string
}

func CheckHealth(ctx context.Context, pool *pgxpool.Pool) DBHealth {
	if pool == nil {
		return DBHealth{
			Status: "unhealthy",
			Error:  "database pool is nil",
		}
	}

	start := time.Now()
	err := pool.Ping(ctx)
	latency := time.Since(start)

	stat := pool.Stat()
	report := DBHealth{
		Status:        "healthy",
		Latency:       latency,
		TotalConns:    stat.TotalConns(),
		IdleConns:     stat.IdleConns(),
		AcquiredConns: stat.AcquiredConns(),
	}

	if err != nil {
		report.Status = "unhealthy"
		report.Error = err.Error()
	}

	return report
}
