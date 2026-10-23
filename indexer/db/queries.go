package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type DbInvoice struct {
	ID              string `json:"id"`
	Issuer          string `json:"issuer"`
	Buyer           string `json:"buyer"`
	FaceValue       string `json:"face_value"` // BigInt represented as string for JSON/SQL numeric safety
	DiscountBps     int    `json:"discount_bps"`
	FundedAmount    string `json:"funded_amount"`
	DueDate         int64  `json:"due_date"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
	FundedAt        *int64 `json:"funded_at"`
	ShippedAt       *int64 `json:"shipped_at"`
	IssuerConfirmed bool   `json:"issuer_confirmed"`
	BuyerConfirmed  bool   `json:"buyer_confirmed"`
	RepaidAt        *int64 `json:"repaid_at"`
}

type DbPoolStats struct {
	TotalDeposits         string    `json:"total_deposits"`
	TotalFunded           string    `json:"total_funded"`
	AvailableLiquidity    string    `json:"available_liquidity"`
	UtilizationRateBps    int       `json:"utilization_rate_bps"`
	TotalYieldDistributed string    `json:"total_yield_distributed"`
	ActiveInvoiceCount    int       `json:"active_invoice_count"`
	UpdatedAt             time.Time `json:"updated_at"`
}

var (
	ErrInvoiceNotFound          = errors.New("invoice not found")
	ErrInvalidInvoiceTransition = errors.New("invalid invoice status transition")
)

func updateInvoice(ctx context.Context, id, operation, targetStatus, query string, args ...any) error {
	tag, err := Pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("queries: %s: %w", operation, err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	var currentStatus string
	err = Pool.QueryRow(ctx, `SELECT status FROM invoices WHERE id = $1`, id).Scan(&currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("queries: %s: %w: %s", operation, ErrInvoiceNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("queries: %s: inspect invoice after update: %w", operation, err)
	}

	return fmt.Errorf("queries: %s: %w: invoice %s is %q, cannot transition to %q", operation, ErrInvalidInvoiceTransition, id, currentStatus, targetStatus)
}

func InsertInvoice(ctx context.Context, inv *DbInvoice) error {
	query := `
		INSERT INTO invoices (
			id, issuer, buyer, face_value, discount_bps, funded_amount, due_date, status, created_at,
			funded_at, shipped_at, issuer_confirmed, buyer_confirmed, repaid_at
		) VALUES (
			@id, @issuer, @buyer, @face_value, @discount_bps, @funded_amount, @due_date, @status, @created_at,
			@funded_at, @shipped_at, @issuer_confirmed, @buyer_confirmed, @repaid_at
		)
	`
	args := pgx.NamedArgs{
		"id":               inv.ID,
		"issuer":           inv.Issuer,
		"buyer":            inv.Buyer,
		"face_value":       inv.FaceValue,
		"discount_bps":     inv.DiscountBps,
		"funded_amount":    inv.FundedAmount,
		"due_date":         inv.DueDate,
		"status":           inv.Status,
		"created_at":       inv.CreatedAt,
		"funded_at":        inv.FundedAt,
		"shipped_at":       inv.ShippedAt,
		"issuer_confirmed": inv.IssuerConfirmed,
		"buyer_confirmed":  inv.BuyerConfirmed,
		"repaid_at":        inv.RepaidAt,
	}
	_, err := Pool.Exec(ctx, query, args)
	if err != nil {
		return fmt.Errorf("queries: insert invoice: %w", err)
	}
	return nil
}

func GetInvoiceByID(ctx context.Context, id string) (*DbInvoice, error) {
	query := `
		SELECT 
			id, issuer, buyer, face_value, discount_bps, funded_amount, due_date, status, created_at,
			funded_at, shipped_at, issuer_confirmed, buyer_confirmed, repaid_at
		FROM invoices
		WHERE id = $1
	`
	row := Pool.QueryRow(ctx, query, id)
	var inv DbInvoice
	err := row.Scan(
		&inv.ID, &inv.Issuer, &inv.Buyer, &inv.FaceValue, &inv.DiscountBps, &inv.FundedAmount, &inv.DueDate, &inv.Status, &inv.CreatedAt,
		&inv.FundedAt, &inv.ShippedAt, &inv.IssuerConfirmed, &inv.BuyerConfirmed, &inv.RepaidAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("queries: get invoice by id: %w", err)
	}
	return &inv, nil
}

func GetInvoices(ctx context.Context, status, issuer string) ([]*DbInvoice, error) {
	query := `
		SELECT 
			id, issuer, buyer, face_value, discount_bps, funded_amount, due_date, status, created_at,
			funded_at, shipped_at, issuer_confirmed, buyer_confirmed, repaid_at
		FROM invoices
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR issuer = $2)
		ORDER BY created_at DESC
	`
	rows, err := Pool.Query(ctx, query, status, issuer)
	if err != nil {
		return nil, fmt.Errorf("queries: get invoices: %w", err)
	}
	defer rows.Close()

	var invoices []*DbInvoice
	for rows.Next() {
		var inv DbInvoice
		err := rows.Scan(
			&inv.ID, &inv.Issuer, &inv.Buyer, &inv.FaceValue, &inv.DiscountBps, &inv.FundedAmount, &inv.DueDate, &inv.Status, &inv.CreatedAt,
			&inv.FundedAt, &inv.ShippedAt, &inv.IssuerConfirmed, &inv.BuyerConfirmed, &inv.RepaidAt,
		)
		if err != nil {
			return nil, fmt.Errorf("queries: scan invoice: %w", err)
		}
		invoices = append(invoices, &inv)
	}
	return invoices, nil
}

func UpdateInvoiceListed(ctx context.Context, id string, status string, discountBps int) error {
	query := `
		UPDATE invoices 
		SET status = $1, discount_bps = $2
		WHERE id = $3 AND status = 'Created'
	`
	return updateInvoice(ctx, id, "update invoice listed", "Listed", query, status, discountBps, id)
}

func UpdateInvoiceFunded(ctx context.Context, id string, status string, fundedAmount string, fundedAt int64) error {
	query := `
		UPDATE invoices 
		SET status = $1, funded_amount = $2, funded_at = $3
		WHERE id = $4 AND status = 'Listed'
	`
	return updateInvoice(ctx, id, "update invoice funded", "Funded", query, status, fundedAmount, fundedAt, id)
}

func UpdateInvoiceShipped(ctx context.Context, id string, status string, shippedAt int64) error {
	query := `
		UPDATE invoices 
		SET status = $1, shipped_at = $2, issuer_confirmed = TRUE
		WHERE id = $3 AND status = 'Funded'
	`
	return updateInvoice(ctx, id, "update invoice shipped", "Active", query, status, shippedAt, id)
}

func UpdateInvoiceDeliveryConfirmed(ctx context.Context, id string, status string) error {
	query := `
		UPDATE invoices 
		SET status = $1, buyer_confirmed = TRUE
		WHERE id = $2 AND status = 'Active'
	`
	return updateInvoice(ctx, id, "update invoice delivery confirmed", "Confirmed", query, status, id)
}

func UpdateInvoiceRepaid(ctx context.Context, id string, status string, repaidAt int64) error {
	query := `
		UPDATE invoices 
		SET status = $1, repaid_at = $2
		WHERE id = $3 AND status = 'Confirmed'
	`
	return updateInvoice(ctx, id, "update invoice repaid", "Repaid", query, status, repaidAt, id)
}

func UpdateInvoiceStatus(ctx context.Context, id string, status string) error {
	query := `
		UPDATE invoices 
		SET status = $1
		WHERE id = $2 AND status IN ('Funded', 'Active', 'Confirmed')
	`
	return updateInvoice(ctx, id, "update invoice status", "Defaulted", query, status, id)
}

func GetPoolStats(ctx context.Context) (*DbPoolStats, error) {
	query := `
		SELECT total_deposits, total_funded, available_liquidity, utilization_rate_bps, total_yield_distributed, active_invoice_count, updated_at
		FROM pool_snapshots
		WHERE id = 1
	`
	row := Pool.QueryRow(ctx, query)
	var stats DbPoolStats
	err := row.Scan(
		&stats.TotalDeposits, &stats.TotalFunded, &stats.AvailableLiquidity,
		&stats.UtilizationRateBps, &stats.TotalYieldDistributed, &stats.ActiveInvoiceCount,
		&stats.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("queries: get pool stats: %w", err)
	}
	return &stats, nil
}

func UpdatePoolStats(ctx context.Context, stats *DbPoolStats) error {
	query := `
		UPDATE pool_snapshots
		SET total_deposits = @total_deposits,
		    total_funded = @total_funded,
		    available_liquidity = @available_liquidity,
		    utilization_rate_bps = @utilization_rate_bps,
		    total_yield_distributed = @total_yield_distributed,
		    active_invoice_count = @active_invoice_count,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`
	args := pgx.NamedArgs{
		"total_deposits":          stats.TotalDeposits,
		"total_funded":            stats.TotalFunded,
		"available_liquidity":     stats.AvailableLiquidity,
		"utilization_rate_bps":    stats.UtilizationRateBps,
		"total_yield_distributed": stats.TotalYieldDistributed,
		"active_invoice_count":    stats.ActiveInvoiceCount,
	}
	_, err := Pool.Exec(ctx, query, args)
	if err != nil {
		return fmt.Errorf("queries: update pool stats: %w", err)
	}
	return nil
}

func LogEvent(ctx context.Context, eventID, contractID string, ledger int32, ledgerClosedAt int64, eventType string, data interface{}) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("queries: log event: marshal data: %w", err)
	}

	query := `
		INSERT INTO events_log (event_id, contract_id, ledger, ledger_closed_at, event_type, data)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (event_id) DO NOTHING
	`
	_, err = Pool.Exec(ctx, query, eventID, contractID, ledger, ledgerClosedAt, eventType, dataBytes)
	if err != nil {
		return fmt.Errorf("queries: log event: %w", err)
	}
	return nil
}

func IsEventProcessed(ctx context.Context, eventID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM events_log WHERE event_id = $1)`
	var exists bool
	err := Pool.QueryRow(ctx, query, eventID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("queries: is event processed: %w", err)
	}
	return exists, nil
}

func GetLatestProcessedLedger(ctx context.Context) (int32, error) {
	query := `SELECT COALESCE(MAX(ledger), 0) FROM events_log`
	var ledger int32
	err := Pool.QueryRow(ctx, query).Scan(&ledger)
	if err != nil {
		return 0, fmt.Errorf("queries: get latest processed ledger: %w", err)
	}
	return ledger, nil
}
