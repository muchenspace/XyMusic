const actionLabels: Readonly<Record<string, string>> = {
  "system.setup.complete": "完成系统初始化",
  "admin.system.settings.apply": "应用系统设置",

  "admin.user.create": "创建用户",
  "admin.user.update": "更新用户",
  "admin.user.status.update": "更新用户状态",
  "admin.user.password.reset": "重置用户密码",
  "admin.user.session.revoke": "撤销用户会话",

  "admin.artist.create": "创建艺术家",
  "admin.artist.update": "更新艺术家",
  "admin.album.create": "创建专辑",
  "admin.album.update": "更新专辑",
  "admin.album.merge": "合并专辑",
  "admin.track.create": "创建歌曲",
  "admin.track.update": "更新歌曲",
  "admin.track.publish": "发布歌曲",
  "admin.track.archive": "归档歌曲",
  "admin.track.restore": "恢复歌曲",
  "admin.track.delete_permanently": "永久删除歌曲",
  "admin.track.lyrics.upsert": "更新歌曲歌词",

  "admin.library-root.create": "创建音乐源",
  "admin.library-root.update": "更新音乐源",
  "admin.library-root.delete": "删除音乐源",
  "admin.library-root.scan": "扫描音乐源",
  "admin.library-root.scan.cancel": "取消音乐源扫描",

  "admin.job.retry": "重试后台任务",
  "admin.job.cancel": "取消后台任务",
  "media.upload.create": "创建媒体上传",
  "media.upload.complete": "完成媒体上传",
  "media.job.retry": "重试媒体处理任务",

  TRACK_UPDATED: "更新歌曲",
  TRACK_METADATA_UPDATED: "更新歌曲元数据",
  TRACK_METADATA_BATCH_UPDATED: "批量更新歌曲元数据",
  TRACK_METADATA_RESTORED: "恢复歌曲元数据",
  TRACK_METADATA_WRITEBACK_QUEUED: "提交元数据写回",
  TRACK_METADATA_WRITEBACK_CANCELLED: "取消元数据写回",
  TRACK_METADATA_WRITEBACK_RETRIED: "重试元数据写回",
  TRACK_METADATA_WRITEBACK_COMPLETED: "完成元数据写回",
  TRACK_METADATA_WRITEBACK_FAILED: "元数据写回失败",
};

const targetTypeLabels: Readonly<Record<string, string>> = {
  system: "系统",
  user: "用户",
  artist: "艺术家",
  album: "专辑",
  track: "歌曲",
  library_root: "音乐源",
  library_scan: "音乐库扫描任务",
  media_upload: "媒体上传",
  media_job: "媒体处理任务",
  track_metadata: "歌曲元数据",
  metadata_writeback_job: "元数据写回任务",
};

const resultLabels: Readonly<Record<string, string>> = {
  SUCCESS: "成功",
  FAILURE: "失败",
};

export function auditActionLabel(action: string): string {
  return actionLabels[action] ?? action;
}

export function auditTargetTypeLabel(targetType: string): string {
  return targetTypeLabels[targetType] ?? targetType;
}

export function auditResultLabel(result: string): string {
  return resultLabels[result.toUpperCase()] ?? result;
}
