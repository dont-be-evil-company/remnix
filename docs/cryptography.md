# Cryptography

A **Sync Master Key** (SMK) wraps event bundles and checkpoints. The SMK is
itself wrapped in one or more **slots**:

- Recovery key: `bech32` `syncsh1…`, shown once at setup, never stored on the remote
- FIDO2 `hmac-secret` (USB HID; NFC is not used)
- YubiKey PIV

Generation manifests are HMAC-signed with a key derived from the SMK.
`metadata/manifest` is also signed. Tampered JSON fails verification and is
not applied.

Key rotation (`syncsh key rotate`) creates `seq+1` and retains the previous
generation so older bundles still decrypt. Running `setup` twice produces two
generations that both claim `seq=1` - recover with `syncsh key recover`.

The daemon cannot prompt for FIDO every minute. Unlock once per session; the
OS keyring holds the SMK.

Remote storage is untrusted: confidentiality depends on the SMK remaining
secret, not on the cloud provider’s access controls.
