// Standalone utility: register the TokenSheep webhook endpoints on the
// configured Pancake store. Idempotent — a GraphQL query first lists the
// store's existing webhooks so we don't duplicate.
//
// Usage (from repo root):
//   go run ./cmd/pancake-webhook-setup \
//     --merchant MER_... \
//     --key      /path/to/private-key.pem \
//     --store    STO_... \
//     --host     free.tokensheep.fun
//
// The tool registers TWO webhooks:
//   test  → https://{host}/api/waffo-pancake/webhook/test  (TestMode: true)
//   prod  → https://{host}/api/waffo-pancake/webhook/prod  (TestMode: false)
//
// NOTE: the second segment must be "prod" (not "live"). The controller
// (controller/topup_waffo_pancake.go WaffoPancakeWebhook) whitelists exactly
// "test" and "prod"; anything else returns 404.
// Both subscribe to the event set the topup controller understands
// (order.completed + subscription.* + refund.*).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	pancake "github.com/waffo-com/waffo-pancake-sdk-go"
)

func main() {
	merchant := flag.String("merchant", "", "MerchantID (MER_...)")
	keyPath := flag.String("key", "", "path to merchant private key PEM")
	store := flag.String("store", "", "StoreID (STO_...)")
	host := flag.String("host", "free.tokensheep.fun", "public host of the API server (webhook URLs are built from this)")
	dryRun := flag.Bool("dry-run", false, "list existing webhooks and the plan, but don't call Add")
	remove := flag.String("remove", "", "webhook id to delete; when set, the tool ONLY removes it and exits (no registration)")
	flag.Parse()

	if *merchant == "" || *keyPath == "" {
		log.Fatalf("--merchant and --key are required")
	}
	if *remove == "" && *store == "" {
		log.Fatalf("--store is required unless --remove is used")
	}

	rawKey, err := os.ReadFile(*keyPath)
	if err != nil {
		log.Fatalf("read private key: %v", err)
	}
	// SDK normalizes PEM / raw base64 / PKCS#1 / PKCS#8 itself.
	client, err := pancake.New(pancake.Config{
		MerchantID: *merchant,
		PrivateKey: strings.TrimSpace(string(rawKey)),
	})
	if err != nil {
		log.Fatalf("pancake client: %v", err)
	}

	ctx := context.Background()

	// --remove short-circuits everything else: delete one webhook by id.
	if *remove != "" {
		fmt.Printf("== removing webhook %s\n", *remove)
		if *dryRun {
			fmt.Printf("   [dry-run] skipped\n")
			return
		}
		if _, err := client.Webhooks.Remove(ctx, pancake.RemoveWebhookParams{ID: *remove}); err != nil {
			log.Fatalf("Webhooks.Remove %s: %v", *remove, err)
		}
		fmt.Printf("   ok  removed %s\n", *remove)
		return
	}

	existing, err := listStoreWebhooks(ctx, client, *store)
	if err != nil {
		log.Fatalf("list webhooks: %v", err)
	}
	fmt.Printf("== existing webhooks on %s (%d) ==\n", *store, len(existing))
	for _, w := range existing {
		fmt.Printf("  %s  %s  test=%v  events=%d\n", w.ID, w.URL, w.TestMode, len(w.Events))
	}

	events := []pancake.WebhookEventType{
		pancake.WebhookEventTypeOrderCompleted,
		pancake.WebhookEventTypeSubscriptionActivated,
		pancake.WebhookEventTypeSubscriptionPaymentSucceeded,
		pancake.WebhookEventTypeSubscriptionCanceling,
		pancake.WebhookEventTypeSubscriptionUncanceled,
		pancake.WebhookEventTypeSubscriptionUpdated,
		pancake.WebhookEventTypeSubscriptionCanceled,
		pancake.WebhookEventTypeSubscriptionPastDue,
		pancake.WebhookEventTypeRefundSucceeded,
		pancake.WebhookEventTypeRefundFailed,
	}

	targets := []struct {
		envSlug  string
		testMode bool
	}{
		{"test", true},
		{"prod", false},
	}

	for _, t := range targets {
		url := fmt.Sprintf("https://%s/api/waffo-pancake/webhook/%s", *host, t.envSlug)
		if hit := matchWebhook(existing, url, t.testMode); hit != nil {
			fmt.Printf("== %s already registered as %s → nothing to do\n", url, hit.ID)
			continue
		}
		fmt.Printf("== registering %s (testMode=%v)\n", url, t.testMode)
		if *dryRun {
			fmt.Printf("   [dry-run] skipped\n")
			continue
		}
		res, err := client.Webhooks.Add(ctx, pancake.AddWebhookParams{
			StoreID:  *store,
			Channel:  pancake.WebhookChannelHTTP,
			URL:      url,
			Events:   events,
			TestMode: t.testMode,
		})
		if err != nil {
			log.Fatalf("Webhooks.Add %s: %v", url, err)
		}
		fmt.Printf("   ok  id=%s\n", res.Webhook.ID)
		for _, w := range res.Warnings {
			fmt.Printf("   warn: %s\n", w.Message)
		}
	}

	fmt.Println("done")
}

// listStoreWebhooks fetches the current webhook set via GraphQL — the SDK's
// Webhooks resource only exposes Add/Update/Remove.
func listStoreWebhooks(ctx context.Context, client *pancake.Client, storeID string) ([]pancake.StoreWebhook, error) {
	// NOTE: storeWebhooks only returns prod (testMode=false) webhooks. The
	// StoreWebhookFilter schema exposes id/channel/url/events/createdAt/
	// updatedAt but NOT testMode, so there is no way to enumerate test-mode
	// webhooks via GraphQL. We rely on the Pancake API's own idempotency:
	// re-Add with identical (storeId, url, testMode, events) returns the same
	// webhook id instead of creating a duplicate — verified against
	// STO_1Mg15QRo7Zp53x2P2Qhzsk on 2026-07-08.
	const q = `
		query ListStoreWebhooks($storeId: String!) {
			storeWebhooks(storeId: $storeId) {
				id storeId channel url events testMode createdAt updatedAt
			}
		}
	`
	res, err := client.GraphQL.Query(ctx, pancake.GraphQLParams{
		Query:     q,
		Variables: map[string]any{"storeId": storeID},
	})
	if err != nil {
		return nil, err
	}
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %+v", res.Errors)
	}
	var payload struct {
		StoreWebhooks []pancake.StoreWebhook `json:"storeWebhooks"`
	}
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		return nil, fmt.Errorf("decode graphql payload: %w (body=%s)", err, string(res.Data))
	}
	return payload.StoreWebhooks, nil
}

func matchWebhook(list []pancake.StoreWebhook, url string, testMode bool) *pancake.StoreWebhook {
	for i := range list {
		if strings.EqualFold(list[i].URL, url) && list[i].TestMode == testMode {
			return &list[i]
		}
	}
	return nil
}

