package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"io"
)

type Cipher interface {
	Encrypt(data []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

type AESCipher struct {
	secret string
}

var _ Cipher = (*AESCipher)(nil)

func (a *AESCipher) Encrypt(toBeEncrypted []byte) ([]byte, error) {
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

func (a *AESCipher) Decrypt(toBeDecrypted []byte) ([]byte, error) {
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

func (a *AESCipher) aead() (cipher.AEAD, error) {
	aesBlock, err := aes.NewCipher([]byte(mdHashing(a.secret)))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(aesBlock)
}

func NewAESCipher(secret string) Cipher {
	return &AESCipher{
		secret: secret,
	}
}

func mdHashing(input string) string {
	byteInput := []byte(input)
	md5Hash := md5.Sum(byteInput)
	return hex.EncodeToString(md5Hash[:])
}
