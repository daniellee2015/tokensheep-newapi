package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type kiroBusTokenResponse struct {
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id"`
	Name               string  `json:"name"`
	Status             int     `json:"status"`
	CreatedTime        int64   `json:"created_time"`
	AccessedTime       int64   `json:"accessed_time"`
	ExpiredTime        int64   `json:"expired_time"`
	RemainQuota        int     `json:"remain_quota"`
	UsedQuota          int     `json:"used_quota"`
	UnlimitedQuota     bool    `json:"unlimited_quota"`
	ModelLimitsEnabled bool    `json:"model_limits_enabled"`
	ModelLimits        string  `json:"model_limits"`
	Group              string  `json:"group"`
	OwnerSystem        string  `json:"owner_system"`
	BusId              *int64  `json:"bus_id,omitempty"`
	TripId             *int64  `json:"trip_id,omitempty"`
	SeatId             *int64  `json:"seat_id,omitempty"`
	RideOrderId        *int64  `json:"ride_order_id,omitempty"`
	ClientKeyId        *int64  `json:"client_key_id,omitempty"`
	ApiRouteProfileId  *int64  `json:"api_route_profile_id,omitempty"`
	QuotaUnit          *string `json:"quota_unit,omitempty"`
	RpmLimit           *int    `json:"rpm_limit,omitempty"`
	ConcurrencyLimit   *int    `json:"concurrency_limit,omitempty"`
	ConversionRevision *string `json:"conversion_revision,omitempty"`
	IdempotencyKey     *string `json:"idempotency_key,omitempty"`
}

func ListKiroBusTokens(c *gin.Context) {
	scope, ok := kiroBusTokenScope(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	tokens, total, err := model.GetTokensByOwnerScope(scope, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]kiroBusTokenResponse, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, newKiroBusTokenResponse(token))
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetKiroBusToken(c *gin.Context) {
	scope, ok := kiroBusTokenScope(c)
	if !ok {
		return
	}
	token, err := model.GetTokenByOwnerScope(kiroBusTokenId(c), scope)
	if err != nil {
		writeKiroBusTokenError(c, err)
		return
	}
	common.ApiSuccess(c, newKiroBusTokenResponse(token))
}

func DisableKiroBusToken(c *gin.Context) {
	scope, ok := kiroBusTokenScope(c)
	if !ok {
		return
	}
	token, err := model.DisableTokenByOwnerScope(kiroBusTokenId(c), scope)
	if err != nil {
		writeKiroBusTokenError(c, err)
		return
	}
	common.ApiSuccess(c, newKiroBusTokenResponse(token))
}

func DeleteKiroBusToken(c *gin.Context) {
	scope, ok := kiroBusTokenScope(c)
	if !ok {
		return
	}
	if err := model.DeleteTokenByOwnerScope(kiroBusTokenId(c), scope); err != nil {
		writeKiroBusTokenError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func kiroBusTokenScope(c *gin.Context) (model.TokenOwnerScope, bool) {
	scope := model.TokenOwnerScope{
		UserId:      c.GetInt("id"),
		Group:       strings.TrimSpace(c.GetString("group")),
		OwnerSystem: model.TokenOwnerSystemKiroBus,
	}
	if scope.UserId <= 0 || scope.Group == "" {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "kirobus token scope unavailable"})
		return model.TokenOwnerScope{}, false
	}
	return scope, true
}

func kiroBusTokenId(c *gin.Context) int {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func writeKiroBusTokenError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "kirobus token unavailable"})
		return
	}
	common.ApiError(c, err)
}

func newKiroBusTokenResponse(token *model.Token) kiroBusTokenResponse {
	return kiroBusTokenResponse{
		Id:                 token.Id,
		UserId:             token.UserId,
		Name:               token.Name,
		Status:             token.Status,
		CreatedTime:        token.CreatedTime,
		AccessedTime:       token.AccessedTime,
		ExpiredTime:        token.ExpiredTime,
		RemainQuota:        token.RemainQuota,
		UsedQuota:          token.UsedQuota,
		UnlimitedQuota:     token.UnlimitedQuota,
		ModelLimitsEnabled: token.ModelLimitsEnabled,
		ModelLimits:        token.ModelLimits,
		Group:              token.Group,
		OwnerSystem:        token.OwnerSystem,
		BusId:              token.BusId,
		TripId:             token.TripId,
		SeatId:             token.SeatId,
		RideOrderId:        token.RideOrderId,
		ClientKeyId:        token.ClientKeyId,
		ApiRouteProfileId:  token.ApiRouteProfileId,
		QuotaUnit:          token.QuotaUnit,
		RpmLimit:           token.RpmLimit,
		ConcurrencyLimit:   token.ConcurrencyLimit,
		ConversionRevision: token.ConversionRevision,
		IdempotencyKey:     token.IdempotencyKey,
	}
}
