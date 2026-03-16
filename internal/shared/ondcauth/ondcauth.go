package ondcauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
)

const ttlSeconds = 3600

func CreateAuthorisationHeader(payload, privateKey, subscriberID, uniqueKeyID string) (string, error) {
	privateKeyBytes, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("decode private key: %w", err)
	}
	log.Printf("[ondcauth] creating header subscriber_id=%s unique_key_id=%s private_key_len=%d",
		subscriberID, uniqueKeyID, len(privateKeyBytes))

	var edKey ed25519.PrivateKey
	switch len(privateKeyBytes) {
	case 64:
		edKey = ed25519.NewKeyFromSeed(privateKeyBytes[:32])
	case 32:
		edKey = ed25519.NewKeyFromSeed(privateKeyBytes)
	default:
		return "", fmt.Errorf("unsupported private key length %d (expected 32 or 64)", len(privateKeyBytes))
	}

	created := time.Now().Unix()
	expires := created + ttlSeconds

	hash := blake2b.Sum512([]byte(payload))
	digest := base64.StdEncoding.EncodeToString(hash[:])

	signingString := fmt.Sprintf("(created): %d\n(expires): %d\ndigest: BLAKE-512=%s",
		created, expires, digest)
	log.Printf("[ondcauth] signing_string:\n%s", signingString)

	signature := ed25519.Sign(edKey, []byte(signingString))
	signatureB64 := base64.StdEncoding.EncodeToString(signature)

	header := fmt.Sprintf(
		`Signature keyId="%s|%s|ed25519",algorithm="ed25519",created="%d",`+
			`expires="%d",headers="(created) (expires) digest",signature="%s"`,
		subscriberID, uniqueKeyID, created, expires, signatureB64)

	log.Printf("[ondcauth] header created keyId=%s|%s|ed25519 created=%d expires=%d sig=%s...",
		subscriberID, uniqueKeyID, created, expires, truncate(signatureB64, 32))

	return header, nil
}

func VerifyAuthorisationHeader(authHeader, payload, publicKey string) error {
	keyID, created, expires, sig, err := parseAuthHeader(authHeader)
	if err != nil {
		return fmt.Errorf("parse auth header: %w", err)
	}
	log.Printf("[ondcauth] verifying keyId=%s created=%s expires=%s", keyID, created, expires)

	now := time.Now().Unix()
	createdInt, err := strconv.ParseInt(created, 10, 64)
	if err != nil {
		return fmt.Errorf("parse created timestamp: %w", err)
	}
	expiresInt, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return fmt.Errorf("parse expires timestamp: %w", err)
	}
	if createdInt > now {
		log.Printf("[ondcauth] header not yet valid: created=%d now=%d", createdInt, now)
		return fmt.Errorf("header not yet valid (created=%d > now=%d)", createdInt, now)
	}
	if now > expiresInt {
		log.Printf("[ondcauth] header expired: expires=%d now=%d", expiresInt, now)
		return fmt.Errorf("header expired (expires=%d < now=%d)", expiresInt, now)
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return fmt.Errorf("decode public key: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length %d (expected %d)", len(pubKeyBytes), ed25519.PublicKeySize)
	}

	hash := blake2b.Sum512([]byte(payload))
	digest := base64.StdEncoding.EncodeToString(hash[:])

	signingString := fmt.Sprintf("(created): %s\n(expires): %s\ndigest: BLAKE-512=%s",
		created, expires, digest)
	log.Printf("[ondcauth] verify signing_string:\n%s", signingString)

	sigBytes, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	if !ed25519.Verify(pubKeyBytes, []byte(signingString), sigBytes) {
		log.Printf("[ondcauth] verification FAILED keyId=%s", keyID)
		return fmt.Errorf("signature verification failed")
	}

	log.Printf("[ondcauth] verification PASSED keyId=%s", keyID)
	return nil
}

func parseAuthHeader(header string) (keyID, created, expires, signature string, err error) {
	header = strings.TrimPrefix(header, "Signature ")

	fields := map[string]string{}
	for _, part := range splitHeaderParts(header) {
		part = strings.TrimSpace(part)
		idx := strings.Index(part, "=")
		if idx < 0 {
			continue
		}
		key := part[:idx]
		val := strings.Trim(part[idx+1:], `"`)
		fields[key] = val
	}

	keyID = fields["keyId"]
	created = fields["created"]
	expires = fields["expires"]
	signature = fields["signature"]

	if created == "" || expires == "" || signature == "" {
		err = fmt.Errorf("missing required fields in auth header (created=%q expires=%q sig_present=%t)",
			created, expires, signature != "")
	}
	return
}

func splitHeaderParts(header string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	for _, ch := range header {
		switch {
		case ch == '"':
			inQuotes = !inQuotes
			current.WriteRune(ch)
		case ch == ',' && !inQuotes:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
