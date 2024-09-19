package models_test

import (
	"testing"

	"github.com/hookdeck/EventKit/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCipher(t *testing.T) {
	cipher := models.NewAESCipher("secret")

	const value = "hello world"

	var err error
	var encrypted []byte

	t.Run("should encrypt", func(t *testing.T) {
		encrypted, err = cipher.Encrypt([]byte(value))
		assert.Nil(t, err)
		assert.NotNil(t, encrypted)
	})

	t.Run("should decrypt", func(t *testing.T) {
		decrypted, err := cipher.Decrypt(encrypted)
		assert.Nil(t, err)
		assert.Equal(t, value, string(decrypted))
	})
}
