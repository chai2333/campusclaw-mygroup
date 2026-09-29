package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"campusclaw/internal/config"
	"github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var schema string

func Open(c config.Config) (*sql.DB, error) {
	opts := mysql.NewConfig()
	opts.User = c.DBUser
	opts.Passwd = c.DBPassword
	opts.Net = "tcp"
	opts.Addr = net.JoinHostPort(c.DBHost, strconv.Itoa(c.DBPort))
	opts.DBName = c.DBName
	opts.ParseTime = true
	opts.Loc = time.UTC
	opts.Timeout = 3 * time.Second
	opts.ReadTimeout = 10 * time.Second
	opts.WriteTimeout = 10 * time.Second
	opts.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
	conn, err := sql.Open("mysql", opts.FormatDSN())
	if err != nil {
		return nil, errors.New("database configuration invalid")
	}
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)
	return conn, nil
}
func Wait(ctx context.Context, conn *sql.DB) error {
	delay := 100 * time.Millisecond
	for {
		if err := conn.PingContext(ctx); err == nil {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("database startup deadline exceeded")
		case <-timer.C:
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}
func Migrate(ctx context.Context, conn *sql.DB) error {
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return errors.New("database migration failed")
		}
	}
	return nil
}
