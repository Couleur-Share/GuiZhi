export const CAPTURE_PROTOCOL_VERSION = 1;
export type CaptureMode = "auto" | "urls" | "text";
export interface CaptureSubmission { requestId: string; input: string; mode: CaptureMode }
export type CaptureItemStatus = "pending" | "processing" | "completed" | "failed" | "canceled" | "duplicate";
export type CaptureErrorCategory = "login_required" | "rate_limited" | "network" | "capture_failed" | "incomplete";
export interface CaptureItemResult { index: number; status: CaptureItemStatus; error?: CaptureErrorCategory }
export interface CaptureProgress { version: number; items: CaptureItemResult[] }
export interface CaptureDelivery extends CaptureSubmission { id: string; createdAt: number; expiresAt: number; itemCount: number }
export interface CaptureReceipt {
  id: string; requestId: string; createdAt: number; expiresAt: number; itemCount: number;
  state: "accepted" | "received" | "expired"; progress: CaptureProgress | null;
}
export interface CaptureDevice { id: string; name: string; kind: "phone" | "shortcut"; active: number; parentId: string | null }
export interface CapturePairing { id: string; expiresAt: number; deviceId?: string; name?: string }
export interface MobileCaptureSettings {
  origin: string; mailboxId: string; paused: boolean; collectionId: string | null;
  connected: boolean; persistent: boolean; lastReceivedAt?: number; error?: string;
}
