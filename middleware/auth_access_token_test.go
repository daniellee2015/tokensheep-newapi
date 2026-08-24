package middleware

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
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestUserAuthProjectsGroupForAccessTokenAuthentication pins the tokensheep
// invariant from `fix(auth): project access token user group`: authenticating
// via the personal access token in `Authorization` puts the user's `group`
// on the gin context, so downstream Kiro.bus channels see the right billing
// group instead of an empty string. The upstream stateless-auth refactor
// pulled the code path through authenticateDashboardRequest, so the test
// now drives UserAuth directly without the retired gin-contrib session store.
func TestUserAuthProjectsGroupForAccessTokenAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}))
	model.DB = database
	accessToken := "12345678901234567890123456789012"
	require.NoError(t, database.Create(&model.User{
		Username: "kirobus-service", Password: "unused", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "kirobus-api", AccessToken: &accessToken,
	}).Error)

	router := gin.New()
	router.GET("/metadata", UserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"group": c.GetString("group")})
	})
	request := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	request.Header.Set("Authorization", accessToken)
	request.Header.Set("New-Api-User", "1")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"group":"kirobus-api"}`, recorder.Body.String())
}
