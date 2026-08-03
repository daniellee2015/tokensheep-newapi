package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenIdempotencyIndexOnlyConstrainsNonEmptyValues(t *testing.T) {
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		common.SetDatabaseTypes(oldMainDatabaseType, oldLogDatabaseType)
	})
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Token{}))

	empty := "   "
	for _, key := range []string{"ordinary-1", "ordinary-2"} {
		token := Token{UserId: 1, Key: key, IdempotencyKey: &empty}
		require.NoError(t, db.Create(&token).Error)
		assert.Nil(t, token.IdempotencyKey)
	}

	idempotencyKey := "kirobus-request-1"
	first := Token{UserId: 1, Key: "kirobus-1", IdempotencyKey: &idempotencyKey}
	require.NoError(t, db.Create(&first).Error)

	duplicate := Token{UserId: 1, Key: "kirobus-duplicate", IdempotencyKey: &idempotencyKey}
	assert.Error(t, db.Create(&duplicate).Error)

	otherUser := Token{UserId: 2, Key: "kirobus-other-user", IdempotencyKey: &idempotencyKey}
	require.NoError(t, db.Create(&otherUser).Error)
}
