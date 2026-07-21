import { z } from "zod";
import type { SetupCompleteInput } from "@/features/setup/domain/models";

const optionalPublicBaseUrl = z.preprocess(
  (value) => typeof value === "string" ? value.trim() : value,
  z.string().url("请输入公开地址的协议、IP 和端口").or(z.literal("")).optional(),
).transform((value) => value || undefined);

const optionalScanInterval = z.preprocess(
  (value) => value === "" ? null : value,
  z.coerce.number().int().min(5).max(10_080).nullable().optional(),
);

export const setupStepSchemas = {
  http: z.object({
    ipv4Host: z.string().trim().ip({ version: "v4", message: "请输入有效的 IPv4 监听 IP" }),
    ipv4Port: z.coerce.number().int().min(1).max(65_535),
    ipv6Host: z.string().trim().ip({ version: "v6", message: "请输入有效的 IPv6 监听 IP" }),
    ipv6Port: z.coerce.number().int().min(1).max(65_535),
    trustedProxyAddresses: z.array(z.string().trim().min(2).max(64)).max(100),
  }),
  paths: z.object({
    migrationsDirectory: z.string().trim().min(1, "请输入数据库迁移目录").max(4_000),
    adminWebDirectory: z.string().trim().min(1, "请输入管理端资源目录").max(4_000),
  }),
  database: z.object({
    host: z.string().trim().min(1, "请输入数据库地址").max(255),
    port: z.coerce.number().int().min(1).max(65_535),
    database: z.string().trim().min(1, "请输入数据库名").max(255),
    username: z.string().trim().min(1, "请输入数据库用户").max(255),
    password: z.string().min(1, "请输入数据库密码").max(2_000),
    sslMode: z.enum(["disable", "prefer", "require", "verify-full"]),
    maxConnections: z.coerce.number().int().min(1).max(100),
  }),
  storage: z.object({
    endpoint: z.string().trim().url("请输入对象存储的协议、IP 和端口").max(2_000),
    region: z.string().trim().min(1, "请输入区域").max(100),
    bucket: z.string().trim().min(1, "请输入 Bucket").max(255),
    accessKeyId: z.string().min(1, "请输入 Access Key").max(500),
    secretAccessKey: z.string().min(1, "请输入 Secret Key").max(2_000),
    forcePathStyle: z.boolean(),
    publicBaseUrl: optionalPublicBaseUrl,
    signedUrlTtlSeconds: z.coerce.number().int().min(30).max(3_600),
    maxUploadBytes: z.coerce.number().int().min(1).max(Number.MAX_SAFE_INTEGER),
  }),
  media: z.object({
    mode: z.enum(["DIRECTORY", "ADVANCED"]),
    directory: z.string().trim().max(2_000),
    ffmpegPath: z.string().trim().max(2_000),
    ffprobePath: z.string().trim().max(2_000),
    fpcalcPath: z.string().trim().max(2_000),
    acoustIdClient: z.string().trim().max(500),
  }).superRefine((value, context) => {
    if (value.fpcalcPath && !value.acoustIdClient) {
      context.addIssue({ code: z.ZodIssueCode.custom, path: ["acoustIdClient"], message: "启用音频指纹时请输入 AcoustID Client ID" });
    }
    if (value.acoustIdClient && !value.fpcalcPath) {
      context.addIssue({ code: z.ZodIssueCode.custom, path: ["fpcalcPath"], message: "启用音频指纹时请输入 fpcalc 路径" });
    }
  }),
  source: z.object({
    name: z.string().trim().min(1, "请输入音源名称").max(120),
    directory: z.string().trim().min(1, "请输入服务端可访问的目录").max(4_000),
    mode: z.enum(["READ_ONLY", "READ_WRITE"]),
    enabled: z.boolean(),
    syncOnStartup: z.boolean(),
    scanIntervalMinutes: optionalScanInterval,
    includePatterns: z.array(z.string().trim().min(1).max(500)).max(100),
    excludePatterns: z.array(z.string().trim().min(1).max(500)).max(100),
  }),
  administrator: z.object({
    username: z.string().trim().regex(/^[A-Za-z0-9_]{3,32}$/, "用户名须为 3–32 位字母、数字或下划线"),
    displayName: z.string().trim().min(1, "请输入显示名称").max(64),
    password: z.string().min(6, "密码至少 6 个字符").max(128, "密码最多 128 个字符"),
  }),
};

export const setupCompleteSchema = z.object({
  http: setupStepSchemas.http,
  paths: setupStepSchemas.paths,
  database: setupStepSchemas.database,
  storage: setupStepSchemas.storage,
  media: setupStepSchemas.media,
  source: setupStepSchemas.source,
  registration: z.object({ enabled: z.boolean() }),
  administrator: setupStepSchemas.administrator,
  databaseAction: z.enum(["reuse_partial", "migrate", "reset"]).optional(),
  storageAction: z.enum(["reuse", "reset"]).optional(),
});

const setupCompleteWithReusedAdministratorSchema = setupCompleteSchema.extend({
  administrator: z.union([
    setupStepSchemas.administrator,
    z.object({
      username: z.literal(""),
      displayName: z.literal(""),
      password: z.literal(""),
    }),
  ]),
});

export interface SetupCompleteValidationContext {
  reusesActiveAdministrator: boolean;
}

export function validateSetupComplete(
  input: unknown,
  context: SetupCompleteValidationContext,
): z.SafeParseReturnType<unknown, SetupCompleteInput> {
  const schema = context.reusesActiveAdministrator
    ? setupCompleteWithReusedAdministratorSchema
    : setupCompleteSchema;
  return schema.safeParse(input) as z.SafeParseReturnType<unknown, SetupCompleteInput>;
}

export function normalizeSetupInput(input: unknown): SetupCompleteInput {
  return setupCompleteSchema.parse(input) as SetupCompleteInput;
}
