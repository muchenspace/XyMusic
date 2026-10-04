import { z } from "zod";
import type { CreateUserInput, UpdateUserInput, UserRole, UserStatus, UserSummary } from "@/features/users/domain/models";

export interface UserEditorForm {
  id: string;
  username: string;
  displayName: string;
  bio: string;
  role: UserRole;
  status: UserStatus;
  password: string;
  version: number;
}

export const userEditorSchema = z.object({
  username: z.string().trim().regex(/^[A-Za-z0-9_]{3,32}$/, "用户名须为 3–32 位字母、数字或下划线"),
  displayName: z.string().trim().min(1, "请输入显示名称").max(100),
  bio: z.string().max(500, "简介最多 500 个字符"),
  role: z.enum(["ADMIN", "USER"]),
  status: z.enum(["ACTIVE", "SUSPENDED", "DELETED"]),
});

/** Validates the editor form and returns field errors keyed by form field. */
export function validateUserEditor(form: UserEditorForm): Record<string, string> {
  const result = userEditorSchema.safeParse(form);
  const fieldErrors: Record<string, string> = {};
  if (!result.success) {
    for (const issue of result.error.issues) fieldErrors[issue.path.join(".")] = issue.message;
  }
  if (!form.id && form.password.length < 6) fieldErrors.password = "初始密码至少 6 个字符";
  return fieldErrors;
}

export type UserSaveCommand =
  | { kind: "create"; input: CreateUserInput }
  | { kind: "update"; userId: string; input: UpdateUserInput };

/**
 * Builds the create/update command from the editor form, sending only fields
 * the administrator actually changed for updates.
 */
export function buildUserSaveCommand(form: UserEditorForm, original: UserSummary | undefined): UserSaveCommand {
  if (!form.id) {
    return {
      kind: "create",
      input: { username: form.username.trim(), displayName: form.displayName.trim(), role: form.role, password: form.password },
    };
  }
  if (!original) throw new Error("没有需要保存的用户字段");
  const input: UpdateUserInput = { expectedVersion: form.version };
  if (form.username.trim() !== original.username) input.username = form.username.trim();
  if (form.displayName.trim() !== original.displayName) input.displayName = form.displayName.trim();
  if (form.bio.trim() !== (original.bio ?? "")) input.bio = form.bio.trim() || null;
  if (form.role !== original.role) input.role = form.role;
  if (form.status !== original.status) input.status = form.status;
  if (Object.keys(input).length === 1) throw new Error("没有需要保存的用户字段");
  return { kind: "update", userId: form.id, input };
}
