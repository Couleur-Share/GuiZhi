export interface ReadingHead {
  sequence: number;
  generation: string;
  digest: string | null;
  manifestToken: string | null;
  keyId: string | null;
}
export interface ReadingPublish {
  sequence: number;
  parentDigest: string | null;
  manifestToken: string;
  objectDigests: string[];
}
export interface ReadingPairClaim {
  deviceId: string;
  credentialHash: string;
  proof: string;
}
export interface ReadingPairStatus {
  pairId: string;
  expiresAt: number;
  claim: ReadingPairClaim | null;
  bundle: string | null;
}
export interface ReadingDesktopStatus {
  available: boolean;
  configured: boolean;
  origin: string | null;
  collectionIds: string[];
  sequence: number;
  pending: boolean;
  busy: boolean;
  error: string | null;
}
