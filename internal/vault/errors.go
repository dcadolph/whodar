package vault

import "errors"

// ErrEncrypted indicates the stored data is encrypted but no key is configured
// to read it. Callers detect it to prompt for a passphrase or point at the key
// environment variables.
var ErrEncrypted = errors.New("vault: data is encrypted but no key is configured")

// ErrKeySize indicates a raw key that is not the required length.
var ErrKeySize = errors.New("vault: key must be 32 bytes")

// ErrKeyMode indicates the configured key cannot read this file: a passphrase
// for a key-sealed file, or a key for a passphrase-sealed file.
var ErrKeyMode = errors.New("vault: wrong key type for this file")

// ErrCorrupt indicates the data is truncated, tampered with, or the key is
// wrong, so authentication failed.
var ErrCorrupt = errors.New("vault: data is corrupt or the key is wrong")
