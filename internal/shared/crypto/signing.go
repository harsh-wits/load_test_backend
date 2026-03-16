package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"golang.org/x/crypto/blake2b"
)

type Signer interface {
	Sign(body []byte, created, expires time.Time) (string, error)
}

type Verifier interface {
	Verify(body []byte, signingString, signatureB64 string, publicKey ed25519.PublicKey) (bool, error)
}

type Ed25519Signer struct {
	privateKey ed25519.PrivateKey
	bapID      string
	keyID      string
}

func NewEd25519Signer(privateKey ed25519.PrivateKey, bapID, keyID string) *Ed25519Signer {
	return &Ed25519Signer{
		privateKey: privateKey,
		bapID:      bapID,
		keyID:      keyID,
	}
}

func (s *Ed25519Signer) Sign(body []byte, created, expires time.Time) (string, error) {
	hash, err := blake2b.New512(nil)
	if err != nil {
		return "", err
	}
	if _, err := hash.Write(body); err != nil {
		return "", err
	}
	digest := hash.Sum(nil)
	digestB64 := base64.StdEncoding.EncodeToString(digest)

	signingString := fmt.Sprintf("(created): %d\n(expires): %d\ndigest: BLAKE-512=%s", created.Unix(), expires.Unix(), digestB64)
	sig := ed25519.Sign(s.privateKey, []byte(signingString))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	header := fmt.Sprintf(`Signature keyId="%s|%s|ed25519",algorithm="ed25519",created="%d",expires="%d",headers="(created) (expires) digest",signature="%s"`,
		s.bapID,
		s.keyID,
		created.Unix(),
		expires.Unix(),
		sigB64,
	)
	return header, nil
}

type Ed25519Verifier struct{}

func NewEd25519Verifier() *Ed25519Verifier {
	return &Ed25519Verifier{}
}

func (v *Ed25519Verifier) Verify(body []byte, signingString, signatureB64 string, publicKey ed25519.PublicKey) (bool, error) {
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return false, err
	}
	ok := ed25519.Verify(publicKey, []byte(signingString), sig)
	return ok, nil
}

// GenerateKeyPair is a helper for local/testing use.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

