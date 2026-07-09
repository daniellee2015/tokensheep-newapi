// Package tokensheep_setting — wallet UX toggles.
//
// TokenSheep splits the wallet "Add Funds" card into two independently
// toggleable sections plus a permanently visible redemption card:
//
//   - EnableTierCardsInRecharge:  the TokensheepTierCards row that shows
//                                 $10/$50/$100/$500 tier upgrade cards.
//                                 Preset amount + tier perks copy.
//   - EnableCustomTopup:          the classic preset amounts + custom
//                                 amount input + payment-method picker
//                                 (i.e. an "any amount you want" path).
//   - EnableCustomAmountInput:    finer-grained switch nested under
//                                 EnableCustomTopup — hides ONLY the free-form
//                                 "custom amount" input while keeping the
//                                 preset amount buttons. Lets operators force
//                                 users onto fixed preset amounts.
//
// All default to true. The redemption card is always visible and lives
// outside "Add Funds"; there is no toggle for it.
//
// See docs/spec/economy-model.md §10.2 and web/default/src/features/wallet/.
package tokensheep_setting

var (
	EnableTierCardsInRecharge = true
	EnableCustomTopup         = true
	EnableCustomAmountInput   = true
)
