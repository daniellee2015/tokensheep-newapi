package common

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func verificationRedisKey(key string, purpose string) string {
	return fmt.Sprintf("newapi:verification:%s:%x", purpose, sha256.Sum256([]byte(key)))
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) error {
	if RedisEnabled {
		if RDB == nil {
			return errors.New("verification code Redis client is not initialized")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Use Redis directly: the generic RedisSet debug logger prints values.
		return RDB.Set(ctx, verificationRedisKey(key, purpose), code, time.Duration(VerificationValidMinutes)*time.Minute).Err()
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
	return nil
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	if RedisEnabled {
		if RDB == nil {
			SysError("verification code Redis client is not initialized")
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		value, err := RDB.Get(ctx, verificationRedisKey(key, purpose)).Result()
		if err != nil {
			if !errors.Is(err, redis.Nil) {
				SysError("failed to read verification code: " + err.Error())
			}
			return false
		}
		return code == value
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	return code == value.code
}

func DeleteKey(key string, purpose string) {
	if RedisEnabled {
		if RDB == nil {
			SysError("verification code Redis client is not initialized")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := RDB.Del(ctx, verificationRedisKey(key, purpose)).Err(); err != nil {
			SysError("failed to delete verification code: " + err.Error())
		}
		return
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
