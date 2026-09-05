import { RelayDatabase, assert, hash, id, identifier, token } from "./database";
export interface Principal { id: string; mailboxId: string; kind: "desktop" | "phone" | "shortcut"; parentId: string | null; active: number }
export class Accounts {
  constructor(public db: RelayDatabase) {}
  create(body: Record<string, unknown>) {
    const requestId = identifier(body.requestId), credential = token(body.credential);
    assert(typeof body.invite === "string" && body.invite.length <= 128, "invalid_invite");
    return this.db.transaction(() => {
      const existing = this.db.get<{ id: string; credential_hash: string }>("SELECT * FROM mailboxes WHERE request_id=?", requestId);
      if (existing) { assert(existing.credential_hash === hash(credential), "request_conflict", 409); return { mailboxId: existing.id }; }
      const invite = this.db.get<{ used_by: string | null }>("SELECT * FROM invites WHERE hash=?", hash(body.invite as string));
      assert(invite && !invite.used_by, "invalid_invite", 403);
      const mailboxId = id();
      this.db.run("INSERT INTO mailboxes(id,request_id,credential_hash) VALUES(?,?,?)", mailboxId, requestId, hash(credential));
      this.db.run("UPDATE invites SET used_by=? WHERE hash=?", mailboxId, hash(body.invite as string));
      return { mailboxId };
    });
  }
  authenticate(credential: string | undefined, pending = false): Principal {
    assert(credential, "unauthorized", 401);
    const fingerprint = hash(credential);
    const desktop = this.db.get<{ id: string }>("SELECT id FROM mailboxes WHERE credential_hash=? AND active=1", fingerprint);
    if (desktop) return { id: desktop.id, mailboxId: desktop.id, kind: "desktop", parentId: null, active: 1 };
    const device = this.db.get<Principal>(`SELECT d.id,d.mailbox_id AS mailboxId,d.kind,d.parent_id AS parentId,d.active FROM devices d
      JOIN mailboxes m ON m.id=d.mailbox_id WHERE d.credential_hash=? AND m.active=1
      AND (d.parent_id IS NULL OR EXISTS(SELECT 1 FROM devices parent WHERE parent.id=d.parent_id AND parent.active=1))`, fingerprint);
    assert(device && (device.active === 1 || pending), "unauthorized", 401);
    return device;
  }
  pair(mailboxId: string, nonce: string) {
    this.db.run("UPDATE pairings SET expires_at=0 WHERE mailbox_id=? AND confirmed=0", mailboxId);
    const pairing = { id: id(), expiresAt: Date.now() + 300000 };
    this.db.run("INSERT INTO pairings(id,mailbox_id,nonce_hash,expires_at) VALUES(?,?,?,?)", pairing.id, mailboxId, hash(token(nonce)), pairing.expiresAt);
    return pairing;
  }
  claim(body: Record<string, unknown>) {
    const pairingId = identifier(body.pairingId), nonce = token(body.nonce), credential = token(body.credential);
    assert(typeof body.name === "string" && body.name.trim().length > 0 && body.name.length <= 60, "invalid_name");
    return this.db.transaction(() => {
      const pair = this.db.get<{ mailbox_id: string; device_id: string | null }>("SELECT * FROM pairings WHERE id=? AND nonce_hash=? AND confirmed=0 AND expires_at>?", pairingId, hash(nonce), Date.now());
      assert(pair, "pairing_expired", 410);
      if (pair.device_id) {
        const existing = this.db.get<{ credential_hash: string }>("SELECT credential_hash FROM devices WHERE id=?", pair.device_id);
        assert(existing?.credential_hash === hash(credential), "pairing_claimed", 409);
        return { id: pair.device_id };
      }
      const count = this.db.get<{ n: number }>("SELECT count(*) AS n FROM devices WHERE mailbox_id=?", pair.mailbox_id)!.n;
      assert(count < 50, "device_limit", 429);
      const deviceId = id();
      this.db.run("INSERT INTO devices(id,mailbox_id,name,kind,credential_hash) VALUES(?,?,?,'phone',?)", deviceId, pair.mailbox_id, (body.name as string).trim(), hash(credential));
      this.db.run("UPDATE pairings SET device_id=? WHERE id=?", deviceId, pairingId);
      return { id: deviceId };
    });
  }
  confirm(mailboxId: string, pairingId: string, deviceId: string) {
    return this.db.transaction(() => {
      const pair = this.db.get<{ confirmed: number }>("SELECT confirmed FROM pairings WHERE id=? AND mailbox_id=? AND device_id=? AND expires_at>?", pairingId, mailboxId, deviceId, Date.now());
      assert(pair, "pairing_expired", 410);
      if (!pair.confirmed) {
        this.db.run("UPDATE devices SET active=1 WHERE id=?", deviceId);
        this.db.run("UPDATE pairings SET confirmed=1 WHERE id=?", pairingId);
      }
      return { success: true };
    });
  }
  shortcut(phone: Principal, credential: string) {
    const fingerprint = hash(token(credential));
    const existing = this.db.get<{ id: string; parent_id: string }>("SELECT * FROM devices WHERE credential_hash=?", fingerprint);
    if (existing) { assert(existing.parent_id === phone.id, "request_conflict", 409); return { id: existing.id }; }
    const count = this.db.get<{ n: number }>("SELECT count(*) AS n FROM devices WHERE mailbox_id=?", phone.mailboxId)!.n;
    assert(count < 50, "device_limit", 429);
    const deviceId = id();
    this.db.run("INSERT INTO devices VALUES(?,?,?,'shortcut',?,?,1)", deviceId, phone.mailboxId, "iPhone Shortcut", fingerprint, phone.id);
    return { id: deviceId };
  }
  revoke(mailboxId: string, deviceId: string) {
    this.db.run("UPDATE devices SET active=0 WHERE mailbox_id=? AND (id=? OR parent_id=?)", mailboxId, deviceId, deviceId);
    this.db.run("UPDATE pairings SET expires_at=0 WHERE mailbox_id=? AND device_id=?", mailboxId, deviceId);
    return { success: true };
  }
}
