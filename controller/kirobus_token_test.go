package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAddTokenPersistsOwnerSystem(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	body := map[string]any{
		"name":              "kirobus-owned",
		"expired_time":      int64(2_000_000_000),
		"unlimited_quota":   true,
		"group":             "kirobus-api",
		"owner_system":      model.TokenOwnerSystemKiroBus,
		"idempotency_key":   "kirobus-owner-test",
		"model_limits":      "gpt-4.1",
		"rpm_limit":         9,
		"concurrency_limit": 2,
	}

	context, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/token/", body, 71)
	AddToken(context)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	var token model.Token
	require.NoError(t, db.Where("user_id = ?", 71).First(&token).Error)
	assert.Equal(t, model.TokenOwnerSystemKiroBus, token.OwnerSystem)
}

func TestListKiroBusTokensScopesAndOmitsKey(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	first := seedOwnedToken(t, db, 10, "kirobus-api", model.TokenOwnerSystemKiroBus, "list-first", "secret-first")
	second := seedOwnedToken(t, db, 10, "kirobus-api", model.TokenOwnerSystemKiroBus, "list-second", "secret-second")
	seedOwnedToken(t, db, 11, "kirobus-api", model.TokenOwnerSystemKiroBus, "other-user", "secret-user")
	seedOwnedToken(t, db, 10, "other-group", model.TokenOwnerSystemKiroBus, "other-group", "secret-group")
	seedOwnedToken(t, db, 10, "kirobus-api", "other-system", "other-owner", "secret-owner")

	context, recorder := newKiroBusTokenContext(t, http.MethodGet, "/api/user/kirobus/tokens?p=1&page_size=1", 10, "kirobus-api", 0)
	ListKiroBusTokens(context)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Page     int                    `json:"page"`
			PageSize int                    `json:"page_size"`
			Total    int                    `json:"total"`
			Items    []kiroBusTokenResponse `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Total)
	assert.Equal(t, 1, response.Data.PageSize)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, second.Id, response.Data.Items[0].Id)
	assert.Equal(t, int64(101), *response.Data.Items[0].BusId)
	assert.Equal(t, int64(202), *response.Data.Items[0].TripId)
	assert.Equal(t, int64(303), *response.Data.Items[0].SeatId)
	assert.Equal(t, int64(404), *response.Data.Items[0].RideOrderId)
	assert.Equal(t, int64(505), *response.Data.Items[0].ClientKeyId)
	assert.Equal(t, int64(606), *response.Data.Items[0].ApiRouteProfileId)
	assert.Equal(t, "gpt-4.1,gpt-4.1-mini", response.Data.Items[0].ModelLimits)
	assert.NotContains(t, recorder.Body.String(), `"key"`)
	assert.NotContains(t, recorder.Body.String(), first.Key)
	assert.NotContains(t, recorder.Body.String(), second.Key)
}

func TestGetKiroBusTokenRequiresOwnerScope(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	owned := seedOwnedToken(t, db, 20, "kirobus-api", model.TokenOwnerSystemKiroBus, "owned", "secret-owned")
	foreignOwner := seedOwnedToken(t, db, 20, "kirobus-api", "other-system", "foreign-owner", "secret-foreign")

	tests := []struct {
		name       string
		tokenId    int
		userId     int
		group      string
		wantStatus int
	}{
		{name: "owned", tokenId: owned.Id, userId: 20, group: "kirobus-api", wantStatus: http.StatusOK},
		{name: "foreign user", tokenId: owned.Id, userId: 21, group: "kirobus-api", wantStatus: http.StatusNotFound},
		{name: "foreign group", tokenId: owned.Id, userId: 20, group: "other-group", wantStatus: http.StatusNotFound},
		{name: "foreign owner", tokenId: foreignOwner.Id, userId: 20, group: "kirobus-api", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, recorder := newKiroBusTokenContext(t, http.MethodGet, "/api/user/kirobus/tokens/"+strconv.Itoa(test.tokenId), test.userId, test.group, test.tokenId)
			GetKiroBusToken(context)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.NotContains(t, recorder.Body.String(), `"key"`)
			assert.NotContains(t, recorder.Body.String(), owned.Key)
		})
	}
}

func TestDisableKiroBusTokenOnlyWritesDisabledStatus(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedOwnedToken(t, db, 30, "kirobus-api", model.TokenOwnerSystemKiroBus, "disable-me", "secret-disable")
	originalQuota := token.RemainQuota
	originalExpiry := token.ExpiredTime
	originalModels := token.ModelLimits

	context, recorder := newKiroBusTokenContext(t, http.MethodPost, "/api/user/kirobus/tokens/"+strconv.Itoa(token.Id)+"/disable", 30, "kirobus-api", token.Id)
	DisableKiroBusToken(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), `"key"`)
	var stored model.Token
	require.NoError(t, db.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, stored.Status)
	assert.Equal(t, originalQuota, stored.RemainQuota)
	assert.Equal(t, originalExpiry, stored.ExpiredTime)
	assert.Equal(t, originalModels, stored.ModelLimits)

	context, recorder = newKiroBusTokenContext(t, http.MethodPost, "/api/user/kirobus/tokens/"+strconv.Itoa(token.Id)+"/disable", 30, "kirobus-api", token.Id)
	DisableKiroBusToken(context)
	assert.Equal(t, http.StatusOK, recorder.Code)

	foreign := seedOwnedToken(t, db, 30, "kirobus-api", "other-system", "do-not-disable", "secret-other")
	context, recorder = newKiroBusTokenContext(t, http.MethodPost, "/api/user/kirobus/tokens/"+strconv.Itoa(foreign.Id)+"/disable", 30, "kirobus-api", foreign.Id)
	DisableKiroBusToken(context)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	var foreignStored model.Token
	require.NoError(t, db.First(&foreignStored, foreign.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, foreignStored.Status)
}

func TestDeleteKiroBusTokenRequiresOwnerScope(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	owned := seedOwnedToken(t, db, 40, "kirobus-api", model.TokenOwnerSystemKiroBus, "delete-me", "secret-delete")
	foreign := seedOwnedToken(t, db, 40, "kirobus-api", "other-system", "keep-me", "secret-keep")

	context, recorder := newKiroBusTokenContext(t, http.MethodDelete, "/api/user/kirobus/tokens/"+strconv.Itoa(owned.Id), 40, "kirobus-api", owned.Id)
	DeleteKiroBusToken(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.ErrorIs(t, db.First(&model.Token{}, owned.Id).Error, gorm.ErrRecordNotFound)
	var deleted model.Token
	require.NoError(t, db.Unscoped().First(&deleted, owned.Id).Error)
	require.True(t, deleted.DeletedAt.Valid)

	context, recorder = newKiroBusTokenContext(t, http.MethodDelete, "/api/user/kirobus/tokens/"+strconv.Itoa(foreign.Id), 40, "kirobus-api", foreign.Id)
	DeleteKiroBusToken(context)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	require.NoError(t, db.First(&model.Token{}, foreign.Id).Error)
}

func seedOwnedToken(t *testing.T, db *gorm.DB, userId int, group string, ownerSystem string, name string, key string) *model.Token {
	t.Helper()
	busId := int64(101)
	tripId := int64(202)
	seatId := int64(303)
	rideOrderId := int64(404)
	clientKeyId := int64(505)
	apiRouteProfileId := int64(606)
	quotaUnit := "quota"
	rpmLimit := 17
	concurrencyLimit := 3
	conversionRevision := "quota-v4"
	idempotencyKey := "kirobus:" + name
	token := &model.Token{
		UserId:             userId,
		Key:                key,
		Status:             common.TokenStatusEnabled,
		Name:               name,
		CreatedTime:        1_700_000_000,
		AccessedTime:       1_700_000_100,
		ExpiredTime:        2_000_000_000,
		RemainQuota:        50_000,
		UsedQuota:          1_000,
		ModelLimitsEnabled: true,
		ModelLimits:        "gpt-4.1,gpt-4.1-mini",
		Group:              group,
		OwnerSystem:        ownerSystem,
		BusId:              &busId,
		TripId:             &tripId,
		SeatId:             &seatId,
		RideOrderId:        &rideOrderId,
		ClientKeyId:        &clientKeyId,
		ApiRouteProfileId:  &apiRouteProfileId,
		QuotaUnit:          &quotaUnit,
		RpmLimit:           &rpmLimit,
		ConcurrencyLimit:   &concurrencyLimit,
		ConversionRevision: &conversionRevision,
		IdempotencyKey:     &idempotencyKey,
	}
	require.NoError(t, db.Create(token).Error)
	return token
}

func newKiroBusTokenContext(t *testing.T, method string, target string, userId int, group string, tokenId int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	context, recorder := newAuthenticatedContext(t, method, target, nil, userId)
	context.Set("group", group)
	if tokenId > 0 {
		context.Params = gin.Params{{Key: "id", Value: strconv.Itoa(tokenId)}}
	}
	return context, recorder
}
