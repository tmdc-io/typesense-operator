package controller

import (
	"crypto/rand"
	"encoding/base64"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateToken() (string, error) {
	token := make([]byte, 256)
	_, err := rand.Read(token)
	if err != nil {
		return "", err
	}

	base64EncodedToken := base64.StdEncoding.EncodeToString(token)
	return base64EncodedToken, nil
}

func generateSecureRandomString(length int) (string, error) {
	result := make([]byte, length)
	_, err := rand.Read(result)
	if err != nil {
		return "", err
	}

	for i := range result {
		result[i] = letters[int(result[i])%len(letters)]
	}
	return string(result), nil
}
