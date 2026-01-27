package redistenantstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"io"
)

type aesCipher struct {
	secret string
}

func (a *aesCipher) encrypt(toBeEncrypted []byte) ([]byte, error) {
	aead, err := a.aead()
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, err
	}

	encrypted := aead.Seal(nonce, nonce, toBeEncrypted, nil)
	return encrypted, nil
}

func (a *aesCipher) decrypt(toBeDecrypted []byte) ([]byte, error) {
	aead, err := a.aead()
	if err != nil {
		return nil, err
	}

	nonceSize := aead.NonceSize()
	nonce, encrypted := toBeDecrypted[:nonceSize], toBeDecrypted[nonceSize:]

	decrypted, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, err
	}

	return decrypted, nil
}

func (a *aesCipher) aead() (cipher.AEAD, error) {
	aesBlock, err := aes.NewCipher([]byte(mdHashing(a.secret)))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(aesBlock)
}

func newAESCipher(secret string) *aesCipher {
	return &aesCipher{secret: secret}
}

func mdHashing(input string) string {
	byteInput := []byte(input)
	md5Hash := md5.Sum(byteInput)
	return hex.EncodeToString(md5Hash[:])
}
