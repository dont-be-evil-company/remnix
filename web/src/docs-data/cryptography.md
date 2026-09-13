---
title: Cryptography
excerpt: Sync Master Key, recovery/FIDO/PIV slots, and signed manifests.
description: How remnix wraps the Sync Master Key, signs manifests, and rotates generations.
order: 3
---

A **Sync Master Key** (SMK) wraps event bundles and checkpoints. The SMK is
itself wrapped in one or more **slots**:

- Recovery key: `bech32` `remnix1...`, shown once at setup, never stored on the remote
- FIDO2 `hmac-secret` (USB HID; NFC is not used)
- YubiKey PIV

Generation manifests are HMAC-signed with a key derived from the SMK.
`metadata/manifest` is also signed. Tampered JSON fails verification and is
not applied.

Key rotation (`remnix key rotate`) creates `seq+1` and retains the previous
generation so older bundles still decrypt. A generation is not considered
active until the authenticated `metadata/manifest` selects it. Rotation is
retry-safe: if a storage or local-state error interrupts the operation,
rerunning the command reconciles any in-progress rotation instead of blindly
creating another generation. Rerun `remnix key rotate` after an interrupted
attempt rather than editing generation files. `remnix key recover` remains the
path for genuine fork or corruption states, not ordinary transient failures.
Old generations stay until garbage collection can prove they are no longer
referenced.

Running `setup` twice produces two generations that both claim `seq=1` -
recover with `remnix key recover`.

The daemon cannot prompt for FIDO every minute. Unlock once per session; the
OS keyring holds the SMK.

Remote storage is untrusted: confidentiality depends on the SMK remaining
secret, not on the cloud provider’s access controls.
