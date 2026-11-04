# Account Registry Schema

The provisioning registry stores non-secret lifecycle metadata. Authentication material is referenced through an external secret store and must never be stored here.

## Record

```json
{
  "id": "account-0001",
  "provider": "the live server",
  "discord_identity_ref": "discord-0001",
  "status": "ready",
  "script": "progressive-fisher",
  "node_id": null,
  "secret_ref": "local-secret-store/account-0001",
  "created_at": "2026-09-14T00:00:00Z",
  "updated_at": "2026-09-14T00:00:00Z"
}
```

## Lifecycle

`pending -> authenticating -> ready -> assigned -> running -> stopped`

Failure states may transition to `disabled` or return to `ready` after remediation.

## TODO
- [ ] Implement persistent registry.
- [ ] Add atomic state transitions.
- [ ] Add schema versioning/migrations.
- [ ] Add secret-store adapter.
- [ ] Add controller handoff API.

## Agent notes
Keep registry operations independent of the thin client. Never persist Discord passwords, tokens, cookies, OAuth secrets, or other authentication material.
