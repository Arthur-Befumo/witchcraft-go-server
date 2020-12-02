// Copyright 2017 Palantir Technologies. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package encryptedconfigvalue

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/palantir/go-encrypted-config-value/encryption"
)

type Encrypter interface {
	// Encrypt returns a new EncryptedValue that is the result of encrypting the provided plaintext using the
	// provided key. The provided key must be a valid encryption key. The returned EncryptedValue will be encrypted
	// using the algorithm that is appropriate for the specified key. Returns an error if the provided key is not
	// a valid encryption key or if an error is encountered during encryption.
	Encrypt(input string, key KeyWithType) (EncryptedValue, error)
}

// KeyPair stores a key pair that can be used to encrypt and decrypt values. For symmetric-key algorithms, the encryption
// key and decryption key will be the same.
type KeyPair struct {
	EncryptionKey KeyWithType
	DecryptionKey KeyWithType
}

// KeyWithType stores a typed key.
type KeyWithType struct {
	Type KeyType
	Key  encryption.Key
}

// SerializedKeyWithType is the serialized string representation of a KeyWithType. It is a string of the form
// "<key-type>:<base64-encoded-key-bytes>".
type SerializedKeyWithType string

func newSerializedKeyWithType(keyType KeyType, keyBytes []byte) SerializedKeyWithType {
	return SerializedKeyWithType(fmt.Sprintf("%s:%s", keyType, base64.StdEncoding.EncodeToString(keyBytes)))
}

// ToSerializable returns the string that can be used to serialize this KeyWithType. The returned string can be used as
// input to the "NewKeyWithType" function to recreate the value. The serialized string is of the form
// "<key-type>:<base64-encoded-key-bytes>".
func (kwt KeyWithType) ToSerializable() SerializedKeyWithType {
	return newSerializedKeyWithType(kwt.Type, kwt.Key.Bytes())
}

// MustNewKeyWithTypeFromSerialized returns the result of calling NewKeyWithTypeFromSerialized with the provided
// arguments. Panics if the call returns an error. This function should only be used when instantiating keys that are
// known to be formatted correctly.
func MustNewKeyWithTypeFromSerialized(input SerializedKeyWithType) KeyWithType {
	return MustNewKeyWithType(string(input))
}

// NewKeyWithTypeFromSerialized returns a new KeyWithType based on the provided SerializedKeyWithType.
func NewKeyWithTypeFromSerialized(input SerializedKeyWithType) (KeyWithType, error) {
	return NewKeyWithType(string(input))
}

// MustNewKeyWithType returns the result of calling NewKeyWithType with the provided arguments. Panics if the call
// returns an error. This function should only be used when instantiating keys that are known to be formatted correctly.
func MustNewKeyWithType(input string) KeyWithType {
	kwt, err := NewKeyWithType(input)
	if err != nil {
		panic(err)
	}
	return kwt
}

// NewKeyWithType returns a new KeyWithType based on the provided string, which must be the serialized form of the key
// compatible with the format generated by ToSerializable. Returns an error if the provided input cannot be parsed as a
// KeyWithType.
//
// For backwards-compatibility, this function also supports deserializing RSA key values that were serialized using the
// "legacy" format. The legacy format for RSA keys was "RSA:<base64-encoded-public-or-private-key>". If the provided
// input is of this form, it will be attempted to be parsed as a private key and then as a public key. If either of
// these operations succeeds, the proper KeyWithType is returned. Otherwise, an error is returned. Note that, although
// legacy values can be read, the returned KeyWithType will be of the new form, and calling "ToSerializable" on the
// returned KeyWithType will return the new form. Serializing RSA keys in the old form is not supported.
func NewKeyWithType(input string) (KeyWithType, error) {
	parts := strings.Split(input, ":")
	if len(parts) != 2 {
		return KeyWithType{}, fmt.Errorf("key must be of the form <algorithm>:<key in base64>, was: %s", input)
	}

	keyBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return KeyWithType{}, fmt.Errorf("failed to base64-decode key: %v", err)
	}

	if parts[0] == "RSA" {
		if privKey, err := RSAPrivateKeyFromBytes(keyBytes); err == nil {
			// legacy private key
			return privKey, nil
		} else if pubKey, err := RSAPublicKeyFromBytes(keyBytes); err == nil {
			// legacy public key
			return pubKey, nil
		}

		// could not parse legacy key
		return KeyWithType{}, fmt.Errorf("unable to parse legacy RSA key")
	}

	alg, err := ToKeyType(parts[0])
	if err != nil {
		return KeyWithType{}, err
	}
	return alg.Generator()(keyBytes)
}
