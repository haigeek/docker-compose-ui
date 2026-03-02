// 服务类型
export type Service = {
  id: string
  name: string
  containerId: string
  status: string
  image: string
}

// 项目类型
export type Project = {
  id: string
  name: string
  composeFilePath?: string
  workingDir?: string
  editable: boolean
  services: Service[]
}

// Compose 文件类型
export type ComposeFile = {
  content: string
  mtime: number
  size: number
  backupPath?: string
}

// API 操作结果类型
export type ActionResult = {
  success: boolean
  message: string
  stdout?: string
  stderr?: string
  durationMs: number
}

// API 错误类型
export type ApiError = {
  code: string
  message: string
  detail?: string
  retryable: boolean
}

// 容器类型
export type ContainerItem = {
  id: string
  name: string
  image: string
  status: string
  project: string
}

// 镜像类型
export type ImageItem = {
  id: string
  repoTags: string[]
  size: number
  created: number
  used: boolean
}

// 镜像删除结果类型
export type ImageDeleteResult = {
  imageId: string
  success: boolean
  message: string
}

// 运行时类型校验辅助函数
function isString(value: unknown): value is string {
  return typeof value === 'string'
}

function isNumber(value: unknown): value is number {
  return typeof value === 'number'
}

function isBoolean(value: unknown): value is boolean {
  return typeof value === 'boolean'
}

function isArray<T>(value: unknown, checkItem: (item: unknown) => item is T): value is T[] {
  return Array.isArray(value) && value.every(checkItem)
}

function assertRequired<T>(value: T | null | undefined, field: string): asserts value is T {
  if (value === null || value === undefined) {
    throw new Error(`缺少必需字段：${field}`)
  }
}

// 校验 Service 类型
export function isService(value: unknown): value is Service {
  if (typeof value !== 'object' || value === null) return false
  const obj = value as Record<string, unknown>
  return (
    isString(obj.id) &&
    isString(obj.name) &&
    isString(obj.containerId) &&
    isString(obj.status) &&
    isString(obj.image)
  )
}

// 校验 Project 类型
export function isProject(value: unknown): value is Project {
  if (typeof value !== 'object' || value === null) return false
  const obj = value as Record<string, unknown>
  return (
    isString(obj.id) &&
    isString(obj.name) &&
    isBoolean(obj.editable) &&
    isArray(obj.services, isService)
  )
}

// 校验 ContainerItem 类型
export function isContainerItem(value: unknown): value is ContainerItem {
  if (typeof value !== 'object' || value === null) return false
  const obj = value as Record<string, unknown>
  return (
    isString(obj.id) &&
    isString(obj.name) &&
    isString(obj.image) &&
    isString(obj.status) &&
    isString(obj.project)
  )
}

// 校验 ImageItem 类型
export function isImageItem(value: unknown): value is ImageItem {
  if (typeof value !== 'object' || value === null) return false
  const obj = value as Record<string, unknown>
  return (
    isString(obj.id) &&
    isArray(obj.repoTags, isString) &&
    isNumber(obj.size) &&
    isNumber(obj.created) &&
    isBoolean(obj.used)
  )
}

// 校验 ComposeFile 类型
export function isComposeFile(value: unknown): value is ComposeFile {
  if (typeof value !== 'object' || value === null) return false
  const obj = value as Record<string, unknown>
  return (
    isString(obj.content) &&
    isNumber(obj.mtime) &&
    isNumber(obj.size)
  )
}

// 校验 ActionResult 类型
export function isActionResult(value: unknown): value is ActionResult {
  if (typeof value !== 'object' || value === null) return false
  const obj = value as Record<string, unknown>
  return (
    isBoolean(obj.success) &&
    isString(obj.message) &&
    isNumber(obj.durationMs)
  )
}

// 安全的 JSON 解析和校验
export function safeParseJSON<T>(raw: string, context = ''): T {
  try {
    return JSON.parse(raw) as T
  } catch (e) {
    throw new Error(`${context}JSON 解析失败：${e instanceof Error ? e.message : String(e)}`)
  }
}

// 带类型校验的 API 响应解析
export function parseAPIResponse<T>(
  data: unknown,
  validator: (value: unknown) => value is T,
  context = ''
): T {
  if (!validator(data)) {
    throw new Error(`${context} 响应数据格式无效`)
  }
  return data
}

// 解析项目列表
export function parseProjects(data: unknown): Project[] {
  const obj = data as Record<string, unknown>
  assertRequired(obj, '响应数据')
  if (!isArray(obj.items, isProject)) {
    return []
  }
  return obj.items
}

// 解析容器列表
export function parseContainers(data: unknown): ContainerItem[] {
  const obj = data as Record<string, unknown>
  assertRequired(obj, '响应数据')
  if (!isArray(obj.items, isContainerItem)) {
    return []
  }
  return obj.items
}

// 解析镜像列表
export function parseImages(data: unknown): ImageItem[] {
  const obj = data as Record<string, unknown>
  assertRequired(obj, '响应数据')
  if (!isArray(obj.items, isImageItem)) {
    return []
  }
  return obj.items
}
