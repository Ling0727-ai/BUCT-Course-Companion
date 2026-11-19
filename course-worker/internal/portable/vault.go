package portable

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

type Account struct {
	StudentID string `json:"studentId"`
	Password  string `json:"password"`
}

const vaultVersion = "BUCT1"
const saltLength = 16

func SealAccount(account Account, passphrase string) ([]byte, error) {
	if utf8.RuneCountInString(passphrase) < 8 {
		return nil, errors.New("凭证保护密码至少 8 个字符")
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	box, err := accountCipher(passphrase, salt)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	cleartext, err := json.Marshal(account)
	if err != nil {
		return nil, err
	}
	defer clear(cleartext)
	header := append(append([]byte(vaultVersion), salt...), nonce...)
	return box.Seal(header, nonce, cleartext, []byte(vaultVersion)), nil
}

func OpenAccount(data []byte, passphrase string) (Account, error) {
	errUnlock := errors.New("凭证保护密码不正确，或文件损坏")
	if len(data) < len(vaultVersion)+saltLength+12+16 || string(data[:len(vaultVersion)]) != vaultVersion {
		return Account{}, errUnlock
	}
	saltEnd := len(vaultVersion) + saltLength
	box, err := accountCipher(passphrase, data[len(vaultVersion):saltEnd])
	if err != nil {
		return Account{}, err
	}
	nonceEnd := saltEnd + box.NonceSize()
	cleartext, err := box.Open(nil, data[saltEnd:nonceEnd], data[nonceEnd:], []byte(vaultVersion))
	if err != nil {
		return Account{}, errUnlock
	}
	defer clear(cleartext)
	var account Account
	if err := json.Unmarshal(cleartext, &account); err != nil || account.StudentID == "" || account.Password == "" {
		return Account{}, errUnlock
	}
	return account, nil
}

func accountCipher(passphrase string, salt []byte) (cipher.AEAD, error) {
	key := argon2.IDKey([]byte(passphrase), salt, 3, 32*1024, 1, 32)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
