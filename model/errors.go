package model

import "errors"

// Common errors
var (
	ErrDatabase = errors.New("database error")
	// ErrInsufficientUserQuota is returned by wallet-debit paths when the
	// combined paid + gift balance cannot cover the requested spend. Callers
	// use errors.Is to detect insufficient-balance instead of string matching.
	ErrInsufficientUserQuota = errors.New("用户额度不足")
)

// User auth errors
var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserEmptyCredentials = errors.New("empty credentials")
	ErrEmailAlreadyTaken    = errors.New("email already taken")
	ErrEmailNotFound        = errors.New("email not found")
	ErrEmailAmbiguous       = errors.New("email matches multiple users")
)

// Token auth errors
var (
	ErrTokenNotProvided = errors.New("token not provided")
	ErrTokenInvalid     = errors.New("token invalid")
)

// Redemption errors
var ErrRedeemFailed = errors.New("redeem.failed")

// 2FA errors
var ErrTwoFANotEnabled = errors.New("2fa not enabled")
var ErrTwoFAAlreadyEnabled = errors.New("2fa already enabled")
