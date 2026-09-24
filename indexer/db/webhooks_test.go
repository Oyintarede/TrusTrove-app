package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// createTestEndpoint inserts a webhook endpoint and registers its cleanup.
// Deliveries cascade on delete, so removing the endpoint clears its queue too.
func createTestEndpoint(
	t *testing.T,
	ctx context.Context,
	eventTypes []string,
	active bool,
) int64 {
	t.Helper()

	url := fmt.Sprintf("https://example.test/hook/%d", time.Now().UnixNano())
	var id int64
	err := Pool.QueryRow(ctx, `
		INSERT INTO webhook_endpoints (url, secret, event_types, active)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, url, "test-secret", eventTypes, active).Scan(&id)
	if err != nil {
		t.Fatalf("insert webhook endpoint: %v", err)
	}

	t.Cleanup(func() {
		if Pool != nil {
			Pool.Exec(ctx, "DELETE FROM webhook_endpoints WHERE id = $1", id)
		}
	})
	return id
}

// findDelivery returns the pending delivery with the given id, or nil.
func findDelivery(t *testing.T, ctx context.Context, id int64) *WebhookDelivery {
	t.Helper()

	deliveries, err := GetPendingDeliveries(ctx, 200)
	if err != nil {
		t.Fatalf("GetPendingDeliveries: %v", err)
	}
	for _, d := range deliveries {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// newDelivery enqueues one delivery for endpointID and returns its row id.
func newDelivery(
	t *testing.T,
	ctx context.Context,
	endpointID int64,
	eventType string,
) int64 {
	t.Helper()

	if err := CreateWebhookDelivery(ctx, endpointID, eventType, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("CreateWebhookDelivery: %v", err)
	}

	var id int64
	err := Pool.QueryRow(ctx, `
		SELECT id FROM webhook_deliveries
		WHERE endpoint_id = $1
		ORDER BY id DESC
		LIMIT 1
	`, endpointID).Scan(&id)
	if err != nil {
		t.Fatalf("look up created delivery: %v", err)
	}
	return id
}

func TestListActiveWebhookEndpointsForEvent(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()
	eventType := fmt.Sprintf("TestEvent%d", time.Now().UnixNano())

	subscribed := createTestEndpoint(t, ctx, []string{eventType}, true)
	wildcard := createTestEndpoint(t, ctx, []string{}, true)
	inactive := createTestEndpoint(t, ctx, []string{eventType}, false)
	other := createTestEndpoint(t, ctx, []string{"SomeOtherEvent"}, true)

	endpoints, err := ListActiveWebhookEndpointsForEvent(ctx, eventType)
	if err != nil {
		t.Fatalf("ListActiveWebhookEndpointsForEvent: %v", err)
	}

	seen := map[int64]*WebhookEndpoint{}
	for _, ep := range endpoints {
		seen[ep.ID] = ep
	}

	if seen[subscribed] == nil {
		t.Error("endpoint subscribed to the event type was not returned")
	}
	if seen[wildcard] == nil {
		t.Error("endpoint with an empty event_types array should receive every event")
	}
	if seen[inactive] != nil {
		t.Error("inactive endpoint must not be returned")
	}
	if seen[other] != nil {
		t.Error("endpoint subscribed to a different event type must not be returned")
	}

	if got := seen[subscribed]; got != nil {
		if got.Secret != "test-secret" {
			t.Errorf("Secret = %q, want %q", got.Secret, "test-secret")
		}
		if len(got.EventTypes) != 1 || got.EventTypes[0] != eventType {
			t.Errorf("EventTypes = %v, want [%s]", got.EventTypes, eventType)
		}
		if got.URL == "" {
			t.Error("URL was not scanned")
		}
	}
}

func TestCreateWebhookDeliveryAndGetPending(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()
	endpointID := createTestEndpoint(t, ctx, []string{}, true)
	eventType := "InvoiceCreated"

	id := newDelivery(t, ctx, endpointID, eventType)

	got := findDelivery(t, ctx, id)
	if got == nil {
		t.Fatal("newly created delivery is not pending")
	}
	if got.EndpointID != endpointID {
		t.Errorf("EndpointID = %d, want %d", got.EndpointID, endpointID)
	}
	if got.EventType != eventType {
		t.Errorf("EventType = %q, want %q", got.EventType, eventType)
	}
	if string(got.Payload) != `{"ok": true}` && string(got.Payload) != `{"ok":true}` {
		t.Errorf("Payload = %q, want the enqueued JSON body", string(got.Payload))
	}
	if got.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0 before the first attempt", got.Attempts)
	}
	if got.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want the schema default of 5", got.MaxAttempts)
	}
	if got.EndpointURL == "" || got.EndpointSecret != "test-secret" {
		t.Errorf("endpoint join did not populate URL/secret: %+v", got)
	}
}

func TestCreateWebhookDeliveryRejectsUnknownEndpoint(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()

	// endpoint_id has a foreign key, so a missing endpoint must surface an error
	// rather than silently enqueueing an undeliverable row.
	err := CreateWebhookDelivery(ctx, -1, "InvoiceCreated", []byte(`{}`))
	if err == nil {
		t.Fatal("expected a foreign-key error for an unknown endpoint, got nil")
	}
}

func TestGetPendingDeliveriesRespectsLimit(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()
	endpointID := createTestEndpoint(t, ctx, []string{}, true)
	newDelivery(t, ctx, endpointID, "InvoiceCreated")
	newDelivery(t, ctx, endpointID, "InvoiceFunded")

	deliveries, err := GetPendingDeliveries(ctx, 1)
	if err != nil {
		t.Fatalf("GetPendingDeliveries: %v", err)
	}
	if len(deliveries) > 1 {
		t.Errorf("returned %d deliveries, want at most 1", len(deliveries))
	}
}

func TestMarkDeliverySuccess(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()
	endpointID := createTestEndpoint(t, ctx, []string{}, true)
	id := newDelivery(t, ctx, endpointID, "InvoiceCreated")

	if err := MarkDeliverySuccess(ctx, id, 200, "accepted"); err != nil {
		t.Fatalf("MarkDeliverySuccess: %v", err)
	}

	var status string
	var attempts, lastStatus int
	var lastResponse string
	err := Pool.QueryRow(ctx, `
		SELECT status, attempts, last_status, last_response
		FROM webhook_deliveries WHERE id = $1
	`, id).Scan(&status, &attempts, &lastStatus, &lastResponse)
	if err != nil {
		t.Fatalf("read back delivery: %v", err)
	}

	if status != "delivered" {
		t.Errorf("status = %q, want %q", status, "delivered")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if lastStatus != 200 {
		t.Errorf("last_status = %d, want 200", lastStatus)
	}
	if lastResponse != "accepted" {
		t.Errorf("last_response = %q, want %q", lastResponse, "accepted")
	}

	// A delivered row must drop out of the pending queue.
	if findDelivery(t, ctx, id) != nil {
		t.Error("delivered delivery is still pending")
	}
}

func TestMarkDeliveryRetry(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()
	endpointID := createTestEndpoint(t, ctx, []string{}, true)
	id := newDelivery(t, ctx, endpointID, "InvoiceCreated")

	statusCode := 503
	next := time.Now().Add(1 * time.Hour)
	if err := MarkDeliveryRetry(ctx, id, next, &statusCode, "upstream unavailable"); err != nil {
		t.Fatalf("MarkDeliveryRetry: %v", err)
	}

	var status string
	var attempts, lastStatus int
	var lastError string
	err := Pool.QueryRow(ctx, `
		SELECT status, attempts, last_status, last_error
		FROM webhook_deliveries WHERE id = $1
	`, id).Scan(&status, &attempts, &lastStatus, &lastError)
	if err != nil {
		t.Fatalf("read back delivery: %v", err)
	}

	if status != "pending" {
		t.Errorf("status = %q, want it to stay %q while retries remain", status, "pending")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if lastStatus != statusCode {
		t.Errorf("last_status = %d, want %d", lastStatus, statusCode)
	}
	if lastError != "upstream unavailable" {
		t.Errorf("last_error = %q, want the recorded message", lastError)
	}

	// Scheduled into the future, so it must not come back as due yet.
	if findDelivery(t, ctx, id) != nil {
		t.Error("delivery scheduled an hour out is still reported as due")
	}
}

func TestMarkDeliveryRetryWithoutStatusCode(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()
	endpointID := createTestEndpoint(t, ctx, []string{}, true)
	id := newDelivery(t, ctx, endpointID, "InvoiceCreated")

	// A transport-level failure (DNS, connection refused) has no HTTP status.
	next := time.Now().Add(30 * time.Second)
	if err := MarkDeliveryRetry(ctx, id, next, nil, "connection refused"); err != nil {
		t.Fatalf("MarkDeliveryRetry: %v", err)
	}

	var lastStatus *int
	var lastError string
	err := Pool.QueryRow(ctx, `
		SELECT last_status, last_error FROM webhook_deliveries WHERE id = $1
	`, id).Scan(&lastStatus, &lastError)
	if err != nil {
		t.Fatalf("read back delivery: %v", err)
	}

	if lastStatus != nil {
		t.Errorf("last_status = %d, want NULL when there was no HTTP response", *lastStatus)
	}
	if lastError != "connection refused" {
		t.Errorf("last_error = %q, want the recorded message", lastError)
	}
}

func TestMarkDeliveryDeadLetter(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()
	endpointID := createTestEndpoint(t, ctx, []string{}, true)
	id := newDelivery(t, ctx, endpointID, "InvoiceCreated")

	if err := MarkDeliveryDeadLetter(ctx, id, "gave up after 5 attempts"); err != nil {
		t.Fatalf("MarkDeliveryDeadLetter: %v", err)
	}

	var status string
	var attempts int
	var lastError string
	err := Pool.QueryRow(ctx, `
		SELECT status, attempts, last_error FROM webhook_deliveries WHERE id = $1
	`, id).Scan(&status, &attempts, &lastError)
	if err != nil {
		t.Fatalf("read back delivery: %v", err)
	}

	if status != "dead_letter" {
		t.Errorf("status = %q, want %q", status, "dead_letter")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if lastError != "gave up after 5 attempts" {
		t.Errorf("last_error = %q, want the recorded message", lastError)
	}

	// Dead-lettered rows must never be retried.
	if findDelivery(t, ctx, id) != nil {
		t.Error("dead-lettered delivery is still pending")
	}
}

func TestMarkDeliveryHelpersIgnoreUnknownIDs(t *testing.T) {
	skipIfNoDB(t)

	ctx := context.Background()

	// These run against ids that no longer exist (endpoint deleted mid-flight).
	// Updating zero rows is not an error, and the worker relies on that.
	if err := MarkDeliverySuccess(ctx, -1, 200, "ok"); err != nil {
		t.Errorf("MarkDeliverySuccess on a missing row: %v", err)
	}
	if err := MarkDeliveryRetry(ctx, -1, time.Now(), nil, "boom"); err != nil {
		t.Errorf("MarkDeliveryRetry on a missing row: %v", err)
	}
	if err := MarkDeliveryDeadLetter(ctx, -1, "boom"); err != nil {
		t.Errorf("MarkDeliveryDeadLetter on a missing row: %v", err)
	}
}
