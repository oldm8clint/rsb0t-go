# Provisioning Integration Boundary

This package boundary is reserved for account lifecycle tooling that runs outside the rsb0t-go thin client.

## Ready-account handoff

The intended flow is:

1. A human completes provider-supported Discord SSO/verification.
2. The provisioner imports or records the resulting account using a secret reference.
3. Validation changes the account state to `ready`.
4. The fleet controller assigns the account to a worker node.
5. The script runtime receives the selected script profile.

No Discord account creation, CAPTCHA solving, authentication bypass, or self-bot behavior belongs in this package.

## TODO
- [ ] Add registry implementation.
- [ ] Add readiness validation implementation.
- [ ] Add controller client/interface.
- [ ] Add lifecycle event emission.
- [ ] Add CLI under `cmd/account-provisioner`.
