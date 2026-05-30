package protocol

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

func GenerateSigningKeyPair() (publicKey string, privateKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}

func GenerateEncryptionKeyPair() (publicKey string, privateKey string, err error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()), base64.StdEncoding.EncodeToString(priv.Bytes()), nil
}

func EncryptPayloadJSON(value any, recipientPublicKey string) (EncryptedPayload, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return EncryptedPayload{}, err
	}
	return EncryptPayload(plain, recipientPublicKey)
}

func EncryptPayload(plain []byte, recipientPublicKey string) (EncryptedPayload, error) {
	recipientBytes, err := base64.StdEncoding.DecodeString(recipientPublicKey)
	if err != nil {
		return EncryptedPayload{}, err
	}
	recipientPub, err := ecdh.X25519().NewPublicKey(recipientBytes)
	if err != nil {
		return EncryptedPayload{}, err
	}
	ephemeralPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return EncryptedPayload{}, err
	}
	shared, err := ephemeralPriv.ECDH(recipientPub)
	if err != nil {
		return EncryptedPayload{}, err
	}
	key := derivePayloadKey(shared, ephemeralPriv.PublicKey().Bytes(), recipientBytes)
	block, err := aes.NewCipher(key)
	if err != nil {
		return EncryptedPayload{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedPayload{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return EncryptedPayload{}, err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	return EncryptedPayload{
		Mode:               "encrypted",
		Algorithm:          "x25519-aes256gcm-sha256kdf",
		ContentType:        "application/json",
		EphemeralPublicKey: base64.StdEncoding.EncodeToString(ephemeralPriv.PublicKey().Bytes()),
		Nonce:              base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:         base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func DecryptPayload(payload EncryptedPayload, recipientPrivateKey string, recipientPublicKey string) ([]byte, error) {
	if payload.Mode != "encrypted" {
		return nil, errors.New("payload is not encrypted")
	}
	privBytes, err := base64.StdEncoding.DecodeString(recipientPrivateKey)
	if err != nil {
		return nil, err
	}
	pubBytes, err := base64.StdEncoding.DecodeString(recipientPublicKey)
	if err != nil {
		return nil, err
	}
	ephBytes, err := base64.StdEncoding.DecodeString(payload.EphemeralPublicKey)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(payload.Nonce)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(payload.Ciphertext)
	if err != nil {
		return nil, err
	}
	priv, err := ecdh.X25519().NewPrivateKey(privBytes)
	if err != nil {
		return nil, err
	}
	ephPub, err := ecdh.X25519().NewPublicKey(ephBytes)
	if err != nil {
		return nil, err
	}
	shared, err := priv.ECDH(ephPub)
	if err != nil {
		return nil, err
	}
	key := derivePayloadKey(shared, ephBytes, pubBytes)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func SignEnvelope(env *Envelope, signingPrivateKey string) error {
	privBytes, err := base64.StdEncoding.DecodeString(signingPrivateKey)
	if err != nil {
		return err
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		return errors.New("invalid ed25519 private key")
	}
	msg, err := envelopeSigningBytes(*env)
	if err != nil {
		return err
	}
	env.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(privBytes), msg))
	return nil
}

func VerifyEnvelopeSignature(env Envelope, signingPublicKey string) error {
	pubBytes, err := base64.StdEncoding.DecodeString(signingPublicKey)
	if err != nil {
		return err
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return errors.New("invalid ed25519 public key")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return err
	}
	msg, err := envelopeSigningBytes(env)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), msg, sig) {
		return errors.New("invalid envelope signature")
	}
	return nil
}

func envelopeSigningBytes(env Envelope) ([]byte, error) {
	env.Signature = ""
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func derivePayloadKey(shared []byte, ephemeralPublic []byte, recipientPublic []byte) []byte {
	h := sha256.New()
	h.Write([]byte("taskferry-payload-v1"))
	h.Write(shared)
	if bytes.Compare(ephemeralPublic, recipientPublic) < 0 {
		h.Write(ephemeralPublic)
		h.Write(recipientPublic)
	} else {
		h.Write(recipientPublic)
		h.Write(ephemeralPublic)
	}
	return h.Sum(nil)
}
