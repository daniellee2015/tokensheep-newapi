package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetKiroBusChannelMetadataIsGroupScopedAndSecretFree(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	model.DB = database
	require.NoError(t, database.AutoMigrate(&model.Channel{}))
	baseURL := "https://pool.example"
	channel := model.Channel{
		Type: 14, Key: "csk_must_not_escape", Status: common.ChannelStatusEnabled,
		Name: "kirobus-api-standard", BaseURL: &baseURL, Group: "kirobus-api,other",
	}
	require.NoError(t, database.Create(&channel).Error)

	t.Run("matching group", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/user/kirobus/channels/1", nil)
		context.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.Id)}}
		context.Set("group", "kirobus-api")

		GetKiroBusChannelMetadata(context)

		var response struct {
			Success bool `json:"success"`
			Data    struct {
				ID     int    `json:"id"`
				Name   string `json:"name"`
				Status int    `json:"status"`
				Group  string `json:"group"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.True(t, response.Success)
		assert.Equal(t, channel.Id, response.Data.ID)
		assert.Equal(t, channel.Name, response.Data.Name)
		assert.Equal(t, channel.Group, response.Data.Group)
		assert.NotContains(t, recorder.Body.String(), channel.Key)
		assert.NotContains(t, recorder.Body.String(), baseURL)
	})

	t.Run("foreign group", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodGet, "/api/user/kirobus/channels/1", nil)
		context.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.Id)}}
		context.Set("group", "foreign")

		GetKiroBusChannelMetadata(context)

		assert.Contains(t, recorder.Body.String(), `"success":false`)
		assert.NotContains(t, recorder.Body.String(), channel.Name)
		assert.NotContains(t, recorder.Body.String(), channel.Key)
	})
}
