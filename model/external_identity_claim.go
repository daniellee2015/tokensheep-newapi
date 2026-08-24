package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ExternalIdentityProviderTelegram = "telegram"

var ErrExternalIdentityAlreadyClaimed = errors.New("external identity is already claimed")

// ExternalIdentityClaim is the durable ownership record for an identity issued
// by an external provider. The two unique indexes make both the provider
// subject and the user's provider slot single-owner without relying on a
// check-then-update sequence.
type ExternalIdentityClaim struct {
	Id        int64     `json:"id" gorm:"primaryKey"`
	Provider  string    `json:"provider" gorm:"type:varchar(32);not null;uniqueIndex:idx_external_identity_subject,priority:1;uniqueIndex:idx_external_identity_user,priority:1"`
	Subject   string    `json:"subject" gorm:"type:varchar(128);not null;uniqueIndex:idx_external_identity_subject,priority:2"`
	UserId    int       `json:"user_id" gorm:"not null;index;uniqueIndex:idx_external_identity_user,priority:2"`
	CreatedAt time.Time `json:"created_at"`
}

func (ExternalIdentityClaim) TableName() string {
	return "external_identity_claims"
}

// ClaimExternalIdentityWithTx atomically claims a provider subject for one
// user. Repeating the exact mapping is idempotent; every competing subject or
// user is rejected. Ownership is read back instead of trusting RowsAffected,
// whose duplicate-key semantics differ between supported databases.
func ClaimExternalIdentityWithTx(tx *gorm.DB, provider, subject string, userId int) error {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	if tx == nil || provider == "" || subject == "" || userId == 0 {
		return errors.New("external identity claim is invalid")
	}

	claim := ExternalIdentityClaim{Provider: provider, Subject: subject, UserId: userId}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
	if result.Error != nil {
		return result.Error
	}
	var subjectOwner ExternalIdentityClaim
	if err := tx.Where("provider = ? AND subject = ?", provider, subject).First(&subjectOwner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrExternalIdentityAlreadyClaimed
		}
		return err
	}
	if subjectOwner.UserId != userId {
		return ErrExternalIdentityAlreadyClaimed
	}

	var userClaim ExternalIdentityClaim
	if err := tx.Where("provider = ? AND user_id = ?", provider, userId).First(&userClaim).Error; err != nil {
		return err
	}
	if userClaim.Subject != subject {
		return ErrExternalIdentityAlreadyClaimed
	}
	return nil
}

func ReleaseExternalIdentityWithTx(tx *gorm.DB, provider string, userId int) error {
	provider = strings.TrimSpace(provider)
	if tx == nil || provider == "" || userId == 0 {
		return errors.New("external identity release is invalid")
	}
	return tx.Where("provider = ? AND user_id = ?", provider, userId).
		Delete(&ExternalIdentityClaim{}).Error
}

func releaseAllExternalIdentitiesWithTx(tx *gorm.DB, userId int) error {
	if tx == nil || userId == 0 {
		return errors.New("external identity release is invalid")
	}
	return tx.Where("user_id = ?", userId).Delete(&ExternalIdentityClaim{}).Error
}

// InitializeExternalIdentityClaims imports legacy Telegram bindings after the
// claim table is migrated.
//
// The upstream implementation failed the entire migration when it encountered
// two users sharing a telegram_id, which blocked every restart on production
// databases that had accumulated ambiguous bindings before telegram_id gained
// a unique constraint. tokensheep prefers to start with a loud warning: the
// oldest user (lowest id) keeps the claim, later duplicates are logged for
// the operator to reconcile, and the rest of the backfill proceeds. The
// warning is not silent — a duplicate telegram_id in `users` means one of
// those accounts cannot log in via Telegram OAuth until an admin picks a
// winner in the admin panel.
func InitializeExternalIdentityClaims() error {
	var users []User
	if err := DB.Unscoped().Select("id", "telegram_id").
		Where("telegram_id <> ?", "").
		Order("id ASC").
		Find(&users).Error; err != nil {
		return err
	}
	// Group by telegram_id so we can pick a winner deterministically (the
	// lowest id, i.e. the earliest-created user) and log the rest.
	claimants := make(map[string][]int, len(users))
	order := make([]string, 0, len(users))
	for _, user := range users {
		if _, seen := claimants[user.TelegramId]; !seen {
			order = append(order, user.TelegramId)
		}
		claimants[user.TelegramId] = append(claimants[user.TelegramId], user.Id)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, telegramId := range order {
			ids := claimants[telegramId]
			winner := ids[0]
			if len(ids) > 1 {
				common.SysError(fmt.Sprintf(
					"telegram_id %q is bound to %d users %v — keeping the oldest (id=%d) and skipping the rest; reconcile in the admin panel to restore Telegram login for the others",
					telegramId, len(ids), ids, winner,
				))
			}
			if err := ClaimExternalIdentityWithTx(tx, ExternalIdentityProviderTelegram, telegramId, winner); err != nil {
				return fmt.Errorf("backfill Telegram identity for user %d: %w", winner, err)
			}
		}
		return nil
	})
}
