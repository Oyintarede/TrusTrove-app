package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInvoiceUpdatesEnforceLifecycle(t *testing.T) {
	databaseURL := os.Getenv("INDEXER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set INDEXER_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create test database pool: %v", err)
	}
	previousPool := Pool
	Pool = pool
	t.Cleanup(func() {
		pool.Close()
		Pool = previousPool
	})
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := RunMigration(ctx); err != nil {
		t.Fatalf("run test database migrations: %v", err)
	}

	tests := []struct {
		name      string
		target    string
		validFrom []string
		staleFrom string
		update    func(context.Context, string) error
	}{
		{
			name:      "listed",
			target:    "Listed",
			validFrom: []string{"Created"},
			staleFrom: "Funded",
			update: func(ctx context.Context, id string) error {
				return UpdateInvoiceListed(ctx, id, "Listed", 250)
			},
		},
		{
			name:      "funded",
			target:    "Funded",
			validFrom: []string{"Listed"},
			staleFrom: "Active",
			update: func(ctx context.Context, id string) error {
				return UpdateInvoiceFunded(ctx, id, "Funded", "100", 1)
			},
		},
		{
			name:      "active",
			target:    "Active",
			validFrom: []string{"Funded"},
			staleFrom: "Confirmed",
			update: func(ctx context.Context, id string) error {
				return UpdateInvoiceShipped(ctx, id, "Active", 1)
			},
		},
		{
			name:      "confirmed",
			target:    "Confirmed",
			validFrom: []string{"Active"},
			staleFrom: "Repaid",
			update: func(ctx context.Context, id string) error {
				return UpdateInvoiceDeliveryConfirmed(ctx, id, "Confirmed")
			},
		},
		{
			name:      "repaid",
			target:    "Repaid",
			validFrom: []string{"Confirmed"},
			staleFrom: "Repaid",
			update: func(ctx context.Context, id string) error {
				return UpdateInvoiceRepaid(ctx, id, "Repaid", 1)
			},
		},
		{
			name:      "defaulted",
			target:    "Defaulted",
			validFrom: []string{"Funded", "Active", "Confirmed"},
			staleFrom: "Defaulted",
			update: func(ctx context.Context, id string) error {
				return UpdateInvoiceStatus(ctx, id, "Defaulted")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name+"/not-found", func(t *testing.T) {
			err := test.update(ctx, newTestInvoiceID())
			if !errors.Is(err, ErrInvoiceNotFound) {
				t.Fatalf("expected ErrInvoiceNotFound, got %v", err)
			}
		})

		t.Run(test.name+"/stale", func(t *testing.T) {
			id := insertTestInvoice(t, ctx, test.staleFrom)
			err := test.update(ctx, id)
			if !errors.Is(err, ErrInvalidInvoiceTransition) {
				t.Fatalf("expected ErrInvalidInvoiceTransition, got %v", err)
			}
			assertInvoiceStatus(t, ctx, id, test.staleFrom)
		})

		for _, source := range test.validFrom {
			t.Run(test.name+"/from-"+source, func(t *testing.T) {
				id := insertTestInvoice(t, ctx, source)
				if err := test.update(ctx, id); err != nil {
					t.Fatalf("update invoice: %v", err)
				}
				assertInvoiceStatus(t, ctx, id, test.target)
			})
		}
	}
}

func insertTestInvoice(t *testing.T, ctx context.Context, status string) string {
	t.Helper()
	id := newTestInvoiceID()
	_, err := Pool.Exec(ctx, `
		INSERT INTO invoices (id, issuer, buyer, face_value, discount_bps, due_date, status, created_at)
		VALUES ($1, 'issuer', 'buyer', 100, 0, 0, $2, 0)
	`, id, status)
	if err != nil {
		t.Fatalf("insert test invoice: %v", err)
	}
	t.Cleanup(func() {
		if _, err := Pool.Exec(context.Background(), `DELETE FROM invoices WHERE id = $1`, id); err != nil {
			t.Errorf("delete test invoice: %v", err)
		}
	})
	return id
}

func assertInvoiceStatus(t *testing.T, ctx context.Context, id, want string) {
	t.Helper()
	var got string
	if err := Pool.QueryRow(ctx, `SELECT status FROM invoices WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read invoice status: %v", err)
	}
	if got != want {
		t.Fatalf("invoice status = %q, want %q", got, want)
	}
}

func newTestInvoiceID() string {
	return fmt.Sprintf("queries-test-%d", time.Now().UnixNano())
}
