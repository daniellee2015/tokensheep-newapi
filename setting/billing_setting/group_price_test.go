package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateGroupModelPrice(t *testing.T) {
	for _, input := range []string{`{}`, `{"call":{"gemini-4-argon":1,"free-model":0}}`} {
		assert.NoError(t, ValidateGroupModelPrice(input))
	}
	for _, input := range []string{`null`, `[]`, `{"":{}}`, `{"call":null}`, `{"call":{"":1}}`, `{"call":{"m":null}}`, `{"call":{"m":-1}}`, `{"call":{"m":1e999}}`, `{"call":{"m":"1"}}`} {
		assert.Error(t, ValidateGroupModelPrice(input), input)
	}
}
