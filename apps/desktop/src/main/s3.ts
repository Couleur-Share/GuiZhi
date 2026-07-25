import {
  GetObjectCommand,
  HeadBucketCommand,
  HeadObjectCommand,
  PutObjectCommand,
  S3Client,
  S3ServiceException,
} from "@aws-sdk/client-s3";
import { NodeHttpHandler } from "@smithy/node-http-handler";
import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { getHttpRequestAgent } from "./services/network-proxy";

interface S3Config {
  endpoint: string;
  region: string;
  bucket: string;
  accessKeyId: string;
  secretAccessKey: string;
}

interface S3ConnectionResult {
  success: boolean;
  message: string;
}

interface S3UploadResult {
  success: boolean;
  error?: string;
}

interface S3DownloadResult {
  success: boolean;
  data?: string;
  notFound?: boolean;
  error?: string;
}

interface S3StatResult {
  success: boolean;
  lastModified?: string;
  notFound?: boolean;
  error?: string;
}

function createS3Client(config: S3Config): S3Client {
  const endpoint = new URL(config.endpoint);
  const agent = getHttpRequestAgent(endpoint);
  return new S3Client({
    region: config.region,
    endpoint: config.endpoint,
    forcePathStyle: true,
    credentials: {
      accessKeyId: config.accessKeyId,
      secretAccessKey: config.secretAccessKey,
    },
    ...(agent
      ? {
          requestHandler: new NodeHttpHandler(
            endpoint.protocol === "https:"
              ? { httpsAgent: agent }
              : { httpAgent: agent },
          ),
        }
      : {}),
  });
}

function normalizeKey(key: string): string {
  return key.replace(/^\/+/, "");
}

function getS3ErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof S3ServiceException) {
    return error.message || fallback;
  }

  if (error instanceof Error) {
    return error.message || fallback;
  }

  return fallback;
}

function isNotFoundError(error: unknown): boolean {
  if (!(error instanceof S3ServiceException)) {
    return false;
  }

  return (
    error.name === "NotFound" ||
    error.name === "NoSuchKey" ||
    error.name === "NoSuchBucket" ||
    error.$metadata?.httpStatusCode === 404
  );
}

export function registerS3IPC(): void {
  ipcMain.handle(
    IPC_CHANNELS.S3_TEST_CONNECTION,
    async (_event, config: S3Config): Promise<S3ConnectionResult> => {
      try {
        const client = createS3Client(config);
        await client.send(new HeadBucketCommand({ Bucket: config.bucket }));
        return {
          success: true,
          message: "Connection successful",
        };
      } catch (error) {
        return {
          success: false,
          message: `Connection failed: ${getS3ErrorMessage(error, "Unknown S3 error")}`,
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.S3_UPLOAD,
    async (
      _event,
      key: string,
      config: S3Config,
      data: string,
    ): Promise<S3UploadResult> => {
      try {
        const client = createS3Client(config);
        await client.send(
          new PutObjectCommand({
            Bucket: config.bucket,
            Key: normalizeKey(key),
            Body: data,
            ContentType: "application/json; charset=utf-8",
          }),
        );

        return { success: true };
      } catch (error) {
        return {
          success: false,
          error: getS3ErrorMessage(error, "Unknown S3 upload error"),
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.S3_DOWNLOAD,
    async (
      _event,
      key: string,
      config: S3Config,
    ): Promise<S3DownloadResult> => {
      try {
        const client = createS3Client(config);
        const response = await client.send(
          new GetObjectCommand({
            Bucket: config.bucket,
            Key: normalizeKey(key),
          }),
        );

        const data = await response.Body?.transformToString();
        if (typeof data !== "string") {
          return {
            success: false,
            error: "S3 object body is empty",
          };
        }

        return { success: true, data };
      } catch (error) {
        if (isNotFoundError(error)) {
          return { success: false, notFound: true };
        }

        return {
          success: false,
          error: getS3ErrorMessage(error, "Unknown S3 download error"),
        };
      }
    },
  );

  ipcMain.handle(
    IPC_CHANNELS.S3_STAT,
    async (_event, key: string, config: S3Config): Promise<S3StatResult> => {
      try {
        const client = createS3Client(config);
        const response = await client.send(
          new HeadObjectCommand({
            Bucket: config.bucket,
            Key: normalizeKey(key),
          }),
        );

        return {
          success: true,
          lastModified: response.LastModified?.toISOString(),
        };
      } catch (error) {
        if (isNotFoundError(error)) {
          return { success: false, notFound: true };
        }

        return {
          success: false,
          error: getS3ErrorMessage(error, "Unknown S3 stat error"),
        };
      }
    },
  );
}
