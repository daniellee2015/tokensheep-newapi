package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLogGroupSelectionListAndStatistics(t *testing.T) {
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalMode := gin.Mode()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		gin.SetMode(originalMode)
	})
	initModelListColumnNames(t)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "logs.db")), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	now := time.Now().Unix()
	logs := []model.Log{
		{UserId: 1, Username: "reader", Group: "gemini-ultra", Type: model.LogTypeConsume, Quota: 20, CreatedAt: now, PromptTokens: 1, CompletionTokens: 2},
		{UserId: 1, Username: "reader", Group: "gemini-ultra-per-call", Type: model.LogTypeConsume, Quota: 10, CreatedAt: now, PromptTokens: 1, CompletionTokens: 2},
		{UserId: 1, Username: "reader", Group: "free", Type: model.LogTypeConsume, Quota: 30, CreatedAt: now, PromptTokens: 1, CompletionTokens: 2},
		{UserId: 2, Username: "other", Group: "gemini-ultra", Type: model.LogTypeConsume, Quota: 100, CreatedAt: now, PromptTokens: 1, CompletionTokens: 2},
	}
	require.NoError(t, db.Create(&logs).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("username", "reader"); c.Next() })
	router.GET("/all", GetAllLogs)
	router.GET("/self", GetUserLogs)
	router.GET("/all/stat", GetLogsStat)
	router.GET("/self/stat", GetLogsSelfStat)
	for _, tc := range []struct {
		name, query                              string
		userTotal, allTotal, userQuota, allQuota int
	}{
		{"multiple groups exclude unrelated groups and keep user scope", "groups=gemini-ultra&groups=gemini-ultra-per-call&groups=gemini-ultra", 2, 3, 30, 130},
		{"legacy single group still works", "group=gemini-ultra-per-call", 1, 1, 10, 10},
		{"unknown selection does not remove the filter", "groups=missing", 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, scope := range []struct {
				path         string
				total, quota int
			}{{"/all", tc.allTotal, tc.allQuota}, {"/self", tc.userTotal, tc.userQuota}} {
				query, err := url.ParseQuery(tc.query)
				require.NoError(t, err)
				query.Set("start_timestamp", strconv.FormatInt(now-10, 10))
				query.Set("end_timestamp", strconv.FormatInt(now+10, 10))
				query.Set("page_size", "1")
				for _, stats := range []bool{false, true} {
					path := scope.path
					if stats {
						path += "/stat"
					}
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?"+query.Encode(), nil))
					require.Equal(t, http.StatusOK, rec.Code)
					var response struct {
						Success bool `json:"success"`
						Data    struct {
							Total int         `json:"total"`
							Items []model.Log `json:"items"`
							Quota int         `json:"quota"`
							Rpm   int         `json:"rpm"`
							Tpm   int         `json:"tpm"`
						} `json:"data"`
					}
					require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &response))
					require.True(t, response.Success, rec.Body.String())
					if stats {
						assert.Equal(t, scope.quota, response.Data.Quota, path)
						assert.Equal(t, scope.total, response.Data.Rpm, path)
						assert.Equal(t, scope.total*3, response.Data.Tpm, path)
					} else {
						assert.Equal(t, scope.total, response.Data.Total, path)
						assert.Len(t, response.Data.Items, min(scope.total, 1), path)
						for _, item := range response.Data.Items {
							assert.NotEqual(t, "free", item.Group)
						}
					}
				}
			}
		})
	}
}
