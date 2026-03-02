<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import loader from '@monaco-editor/loader'
import * as monaco from 'monaco-editor'
import * as prettier from 'prettier/standalone'
import prettierPluginEstree from 'prettier/plugins/estree'
import prettierPluginYaml from 'prettier/plugins/yaml'
import type { editor } from 'monaco-editor'
import { isAbortError, useSSEStream, type StreamStatus } from './composables/useSSEStream'
import { useDebounce } from './composables/useDebounce'
import {
  AuthRequiredError,
  deleteImages,
  getBasicAuthUser,
  getComposeFile,
  getLogs,
  hasBasicAuth,
  listContainers,
  listImages,
  listProjects,
  loginBasicAuth,
  logoutBasicAuth,
  saveComposeFile,
  streamProjectAction,
  streamContainerLogs,
  serviceAction,
} from './api'
import type { ContainerItem, ImageItem, Project, Service } from './types'

loader.config({ monaco })

// --- 状态管理 ---
const projects = ref<Project[]>([])
const authed = ref(hasBasicAuth())
const authLoading = ref(false)
const authError = ref('')
const loginUser = ref(getBasicAuthUser() || 'admin')
const loginPass = ref('')
const loading = ref(false)
const currentMenu = ref<'projects' | 'containers' | 'images'>('projects')
const activeProjectId = ref('')
const composeText = ref('')
const composeMtime = ref(0)

// Toast 消息队列
interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'info' | 'warning'
}
const toasts = ref<Toast[]>([])
let toastId = 0

function addToast(message: string, type: Toast['type'] = 'info', duration = 3000) {
  const id = ++toastId
  toasts.value.push({ id, message, type })
  if (duration) {
    setTimeout(() => removeToast(id), duration)
  }
}

function removeToast(id: number) {
  const idx = toasts.value.findIndex((t) => t.id === id)
  if (idx !== -1) toasts.value.splice(idx, 1)
}

function success(message: string) { addToast(message, 'success') }
function error(message: string) { addToast(message, 'error') }
function info(message: string) { addToast(message, 'info') }

const selectedService = ref<Service | null>(null)
const selectedContainer = ref<ContainerItem | null>(null)
const logs = ref('')
const containers = ref<ContainerItem[]>([])
const containerKeyword = ref('')
const debouncedContainerKeyword = useDebounce(containerKeyword, 300)
const images = ref<ImageItem[]>([])
const imageKeyword = ref('')
const debouncedImageKeyword = useDebounce(imageKeyword, 300)
const imageUsedFilter = ref<'all' | 'used' | 'unused'>('all')
const selectedImageIds = ref<string[]>([])
const imageLoading = ref(false)
const showLogDrawer = ref(false)
const autoFollowLogs = ref(true)
const actionLogs = ref('')
const actionRunning = ref(false)
const actionType = ref<'start' | 'stop' | 'redeploy' | ''>('')
const actionLogHost = ref<HTMLElement | null>(null)
const editorHost = ref<HTMLElement | null>(null)
const logEditorHost = ref<HTMLElement | null>(null)

let composeEditor: editor.IStandaloneCodeEditor | null = null
let editorInitPromise: Promise<void> | null = null
let logEditor: editor.IStandaloneCodeEditor | null = null
let logEditorInitPromise: Promise<void> | null = null

// SSE 流 - 启用自动重连
const logStream = useSSEStream({ maxRetries: 3, retryDelay: 1500 })
const actionStream = useSSEStream({ maxRetries: 0 })

// --- 计算属性 ---
const activeProject = computed(() => {
  const items = Array.isArray(projects.value) ? projects.value : []
  return items.find((p) => p.id === activeProjectId.value) ?? null
})

const allImagesSelected = computed(() => images.value.length > 0 && selectedImageIds.value.length === images.value.length)

// --- 认证处理 ---
function handleAuthError(err: unknown): boolean {
  if (!(err instanceof AuthRequiredError)) return false
  authed.value = false
  authLoading.value = false
  authError.value = err.reason === 'invalid' ? '认证失效，请重新登录' : '请先登录'
  closeActionStream()
  closeLogStream()
  return true
}

async function bootstrapApp() {
  await loadProjects()
  await loadContainers()
  await loadImages()
  await nextTick()
  await initEditor()
  await loadCompose()
}

async function submitLogin() {
  authLoading.value = true
  authError.value = ''
  try {
    await loginBasicAuth(loginUser.value, loginPass.value)
    authed.value = true
    loginPass.value = ''
    success('登录成功')
    await bootstrapApp()
  } catch (e) {
    authed.value = false
    authError.value = String(e)
    error('登录失败')
  } finally {
    authLoading.value = false
  }
}

function logout() {
  logoutBasicAuth()
  authed.value = false
  authError.value = ''
  projects.value = []
  containers.value = []
  images.value = []
  activeProjectId.value = ''
  closeActionStream()
  closeLogStream()
  success('已退出登录')
}

function switchMenu(menu: 'projects' | 'containers' | 'images') {
  currentMenu.value = menu
  closeLogDrawer()
  closeActionStream()
  if (menu !== 'projects') disposeEditor()
  if (menu === 'images') void loadImages()
  if (menu === 'containers') void loadContainers()
  if (menu === 'projects') void loadProjects()
}

// --- 编辑器管理 ---
function getComposeContent(): string {
  return composeEditor?.getValue() ?? composeText.value
}

function setComposeContent(value: string) {
  composeText.value = value
  if (composeEditor && composeEditor.getValue() !== value) {
    composeEditor.setValue(value)
  }
}

function updateEditorReadonly() {
  composeEditor?.updateOptions({ readOnly: !activeProject.value?.editable })
}

async function initEditor() {
  if (composeEditor || editorInitPromise) {
    await editorInitPromise
    return
  }
  if (!editorHost.value) return

  editorInitPromise = (async () => {
    const monacoInstance = await loader.init()
    const host = editorHost.value
    if (!host) return

    for (const ed of monacoInstance.editor.getEditors()) {
      if (ed.getDomNode() === host) {
        ed.dispose()
      }
    }

    composeEditor = monacoInstance.editor.create(host, {
      value: composeText.value,
      language: 'yaml',
      theme: 'vs',
      automaticLayout: true,
      minimap: { enabled: false },
      fontSize: 13,
      tabSize: 2,
      wordWrap: 'on',
      readOnly: !activeProject.value?.editable,
    })
    composeEditor.onDidChangeModelContent(() => {
      composeText.value = composeEditor?.getValue() ?? ''
    })
  })()
  try {
    await editorInitPromise
  } finally {
    editorInitPromise = null
  }
}

function disposeEditor() {
  editorInitPromise = null
  if (composeEditor) {
    composeEditor.dispose()
    composeEditor = null
  }
}

async function initLogEditor() {
  if (logEditor || logEditorInitPromise) {
    await logEditorInitPromise
    return
  }
  if (!logEditorHost.value) return

  logEditorInitPromise = (async () => {
    const monacoInstance = await loader.init()
    const host = logEditorHost.value
    if (!host) return

    for (const ed of monacoInstance.editor.getEditors()) {
      if (ed.getDomNode() === host) {
        ed.dispose()
      }
    }

    logEditor = monacoInstance.editor.create(host, {
      value: logs.value,
      language: 'plaintext',
      theme: 'vs-dark',
      readOnly: true,
      automaticLayout: true,
      minimap: { enabled: false },
      lineNumbers: 'off',
      wordWrap: 'on',
      fontSize: 12,
      scrollBeyondLastLine: false,
    })
  })()

  try {
    await logEditorInitPromise
  } finally {
    logEditorInitPromise = null
  }
}

function syncLogEditor() {
  if (!logEditor) return
  const next = logs.value
  if (logEditor.getValue() !== next) {
    logEditor.setValue(next)
  }
  if (autoFollowLogs.value) {
    const model = logEditor.getModel()
    if (!model) return
    logEditor.revealLine(model.getLineCount())
  }
  logEditor.layout()
}

function disposeLogEditor() {
  logEditorInitPromise = null
  if (logEditor) {
    logEditor.dispose()
    logEditor = null
  }
}

// --- 数据加载 ---
async function loadProjects() {
  loading.value = true
  try {
    const items = await listProjects()
    projects.value = Array.isArray(items) ? items : []
    if (!activeProjectId.value && projects.value.length > 0) {
      activeProjectId.value = projects.value[0].id
    }
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  } finally {
    loading.value = false
  }
}

async function loadContainers() {
  try {
    const items = await listContainers(debouncedContainerKeyword.value)
    containers.value = items
    if (selectedContainer.value) {
      selectedContainer.value = items.find((item) => item.id === selectedContainer.value?.id) ?? null
    }
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

async function loadImages() {
  imageLoading.value = true
  try {
    images.value = await listImages(debouncedImageKeyword.value, imageUsedFilter.value)
    const current = new Set(images.value.map((i) => i.id))
    selectedImageIds.value = selectedImageIds.value.filter((id) => current.has(id))
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  } finally {
    imageLoading.value = false
  }
}

// --- 镜像操作 ---
function toggleImageSelection(id: string, checked: boolean) {
  if (checked) {
    if (!selectedImageIds.value.includes(id)) {
      selectedImageIds.value.push(id)
    }
    return
  }
  selectedImageIds.value = selectedImageIds.value.filter((item) => item !== id)
}

function toggleSelectAllImages(checked: boolean) {
  selectedImageIds.value = checked ? images.value.map((i) => i.id) : []
}

function onToggleSelectAllImages(event: Event) {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  toggleSelectAllImages(checked)
}

function onToggleImage(event: Event, imageID: string) {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  toggleImageSelection(imageID, checked)
}

async function deleteSelectedImages() {
  if (selectedImageIds.value.length === 0) {
    info('请先选择要删除的镜像')
    return
  }
  const confirmed = window.confirm(`确认删除选中的 ${selectedImageIds.value.length} 个镜像吗？`)
  if (!confirmed) return
  try {
    const results = await deleteImages(selectedImageIds.value, false)
    const failed = results.filter((r) => !r.success)
    if (failed.length === 0) {
      success(`已删除 ${results.length} 个镜像`)
    } else {
      info(`删除完成，失败 ${failed.length} 个`)
    }
    await loadImages()
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

async function deleteAllDanglingImages() {
  try {
    const all = await listImages('', 'all')
    const target = all.filter((img) => img.repoTags.some((tag) => tag === '<none>:<none>')).map((img) => img.id)
    if (target.length === 0) {
      info('没有可删除的空镜像')
      return
    }
    const confirmed = window.confirm(`确认删除所有空镜像吗？共 ${target.length} 个`)
    if (!confirmed) return
    const results = await deleteImages(target, false)
    const failed = results.filter((r) => !r.success).length
    success(failed === 0 ? `已删除空镜像 ${results.length} 个` : `空镜像删除完成，失败 ${failed} 个`)
    await loadImages()
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

async function deleteAllUnusedImages() {
  try {
    const unused = await listImages('', 'unused')
    const target = unused.map((img) => img.id)
    if (target.length === 0) {
      info('没有可删除的未使用镜像')
      return
    }
    const confirmed = window.confirm(`确认删除所有未使用镜像吗？共 ${target.length} 个`)
    if (!confirmed) return
    const results = await deleteImages(target, false)
    const failed = results.filter((r) => !r.success).length
    success(failed === 0 ? `已删除未使用镜像 ${results.length} 个` : `未使用镜像删除完成，失败 ${failed} 个`)
    await loadImages()
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

// --- 工具函数 ---
function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let idx = 0
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024
    idx++
  }
  return `${size.toFixed(size >= 10 || idx === 0 ? 0 : 1)} ${units[idx]}`
}

function formatCreated(ts: number): string {
  if (!Number.isFinite(ts) || ts <= 0) return '-'
  return new Date(ts * 1000).toLocaleString()
}

function containerStatusClass(status: string): string {
  const normalized = status.trim().toLowerCase()
  if (normalized.includes('restarting')) return 'tag-status-restarting'
  if (normalized.includes('paused')) return 'tag-status-paused'
  if (normalized.includes('running') || normalized.includes('up')) return 'tag-status-running'
  if (normalized.includes('created')) return 'tag-status-created'
  if (normalized.includes('stopped') || normalized.includes('exited') || normalized.includes('dead')) return 'tag-status-stopped'
  return 'tag-status-unknown'
}

function containerStatusLabel(status: string): string {
  const normalized = status.trim().toLowerCase()
  if (normalized.includes('restarting')) return '重启中'
  if (normalized.includes('paused')) return '已暂停'
  if (normalized.includes('running') || normalized.includes('up')) return '运行中'
  if (normalized.includes('created')) return '已创建'
  if (normalized.includes('stopped') || normalized.includes('exited') || normalized.includes('dead')) return '已停止'
  return status || '未知'
}

// --- Compose 文件操作 ---
async function loadCompose() {
  if (!activeProject.value) return
  await nextTick()
  await initEditor()
  if (!activeProject.value.editable) {
    setComposeContent('# 该项目未关联可编辑 compose 文件')
    updateEditorReadonly()
    composeEditor?.layout()
    return
  }
  try {
    const file = await getComposeFile(activeProject.value.id)
    setComposeContent(file.content)
    composeMtime.value = file.mtime
    updateEditorReadonly()
    composeEditor?.layout()
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

async function formatCompose() {
  if (!activeProject.value || !activeProject.value.editable) return
  try {
    const formatted = await prettier.format(getComposeContent(), {
      parser: 'yaml',
      plugins: [prettierPluginYaml, prettierPluginEstree],
      tabWidth: 2,
    })
    setComposeContent(formatted)
    success('compose 格式化完成')
  } catch (e) {
    if (handleAuthError(e)) return
    error(`格式化失败：${String(e)}`)
  }
}

async function saveCompose() {
  if (!activeProject.value || !activeProject.value.editable) return
  try {
    const file = await saveComposeFile(activeProject.value.id, getComposeContent(), composeMtime.value)
    composeMtime.value = file.mtime
    success(file.backupPath ? `保存成功，备份：${file.backupPath}` : '保存成功')
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

// --- 项目操作 ---
async function redeployProject() {
  if (!activeProject.value) return
  await runProjectOperation('redeploy')
}

function actionTypeLabel(action: 'start' | 'stop' | 'redeploy' | ''): string {
  if (action === 'start') return '启动组'
  if (action === 'stop') return '停止组'
  if (action === 'redeploy') return '重部署'
  return ''
}

function appendActionLog(line: string) {
  actionLogs.value = actionLogs.value ? `${actionLogs.value}\n${line}` : line
}

function closeActionStream() {
  actionStream.stop()
}

function syncActionLogScroll() {
  const host = actionLogHost.value
  if (!host) return
  host.scrollTop = host.scrollHeight
}

async function runProjectOperation(action: 'start' | 'stop' | 'redeploy') {
  if (!activeProject.value || actionRunning.value) return
  actionType.value = action
  actionRunning.value = true
  actionLogs.value = ''
  closeActionStream()
  appendActionLog(`[${new Date().toLocaleTimeString()}] ${actionTypeLabel(action)} 开始`)

  try {
    let done = false
    let failed = false
    await actionStream.start(async (signal) => {
      await streamProjectAction(
        activeProject.value!.id,
        action,
        (event, data) => {
          if (event === 'log') {
            appendActionLog(data)
            return
          }
          if (event === 'action-error') {
            failed = true
            appendActionLog(`ERROR: ${data || '执行失败'}`)
            return
          }
          if (event === 'done') {
            done = data === 'ok'
            if (!done) failed = true
          }
        },
        signal,
        () => {
          // 连接成功建立
          actionStream.setStatus('connected')
        }
      )
    })

    if (!done || failed) {
      throw new Error('操作未完成')
    }

    success(`${actionTypeLabel(action)} 完成`)
    await loadProjects()
    await loadContainers()
    await loadImages()
  } catch (e) {
    if (isAbortError(e)) return
    if (handleAuthError(e)) return
    error(String(e))
  } finally {
    actionRunning.value = false
    closeActionStream()
    appendActionLog(`[${new Date().toLocaleTimeString()}] ${actionTypeLabel(action)} 结束`)
  }
}

async function runProjectAction(action: 'start' | 'stop') {
  if (!activeProject.value) return
  await runProjectOperation(action)
}

// --- 服务/容器操作 ---
async function runServiceAction(action: 'stop' | 'restart' | 'delete') {
  if (!selectedService.value) return
  try {
    const res = await serviceAction(selectedService.value.id, action)
    success(`${selectedService.value.name} ${action} 完成：${res.message}`)
    await loadProjects()
    await loadContainers()
    await loadImages()
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

async function runServiceActionFor(service: Service, action: 'stop' | 'restart' | 'delete') {
  selectedService.value = service
  await runServiceAction(action)
}

async function openServiceLogs(service: Service) {
  selectedService.value = service
  await openLogDrawer()
}

async function runContainerAction(action: 'stop' | 'restart' | 'delete') {
  if (!selectedContainer.value) return
  try {
    const res = await serviceAction(selectedContainer.value.id, action)
    success(`${selectedContainer.value.name} ${action} 完成：${res.message}`)
    await loadProjects()
    await loadContainers()
    await loadImages()
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

async function runContainerActionFor(item: ContainerItem, action: 'stop' | 'restart' | 'delete') {
  selectedContainer.value = item
  await runContainerAction(action)
}

async function openContainerLogs(item: ContainerItem) {
  selectedContainer.value = item
  await openLogDrawer()
}

function getActiveLogTarget(): { name: string; containerId: string } | null {
  if (currentMenu.value === 'containers' && selectedContainer.value) {
    return { name: selectedContainer.value.name, containerId: selectedContainer.value.id }
  }
  if (selectedService.value) {
    return { name: selectedService.value.name, containerId: selectedService.value.containerId }
  }
  return null
}

async function loadLogs() {
  const target = getActiveLogTarget()
  if (!target) return
  try {
    logs.value = await getLogs(target.containerId)
  } catch (e) {
    if (handleAuthError(e)) return
    logs.value = String(e)
  }
}

function getLogStreamStatus(): StreamStatus {
  return logStream.status.value
}

function openLogStream() {
  closeLogStream()
  const target = getActiveLogTarget()
  if (!target) return

  void logStream
    .start(async (signal) => {
      await streamContainerLogs(
        target.containerId,
        (event, data) => {
          if (event === 'log') {
            logs.value += `\n${data}`
            return
          }
          if (event === 'error') {
            info(data || '日志流连接异常')
          }
        },
        signal,
        () => {
          // 连接成功建立
          logStream.setStatus('connected')
        }
      )
    })
    .catch((e) => {
      if (isAbortError(e)) return
      if (handleAuthError(e)) return
      error(`日志流连接断开：${String(e)}`)
    })
}

function closeLogStream() {
  logStream.stop()
}

async function openLogDrawer() {
  if (!getActiveLogTarget()) {
    info(currentMenu.value === 'containers' ? '请先在容器列表中选择容器' : '请先在列表中选择服务')
    return
  }
  showLogDrawer.value = true
  await nextTick()
  await initLogEditor()
  await loadLogs()
  syncLogEditor()
  // 默认开启实时流
  openLogStream()
}

function closeLogDrawer() {
  showLogDrawer.value = false
  closeLogStream()
  disposeLogEditor()
}

// --- 导航 ---
function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (!showLogDrawer.value) return
  closeLogDrawer()
}

function chooseProject(id: string) {
  activeProjectId.value = id
  selectedService.value = null
  logs.value = ''
  actionLogs.value = ''
  actionType.value = ''
  actionRunning.value = false
  showLogDrawer.value = false
  closeActionStream()
  closeLogStream()
  void loadCompose()
}

function chooseContainer(item: ContainerItem) {
  selectedContainer.value = item
}

async function jumpToProjectFromContainer(item: ContainerItem) {
  const projectName = item.project.trim()
  if (!projectName) {
    info('该容器未关联 Compose 项目')
    return
  }
  try {
    await loadProjects()
    const target =
      projects.value.find((p) => p.name === projectName) ??
      projects.value.find((p) => p.name.toLowerCase() === projectName.toLowerCase())
    if (!target) {
      error(`未找到项目：${projectName}`)
      return
    }
    currentMenu.value = 'projects'
    chooseProject(target.id)
    success(`已跳转到项目：${target.name}`)
  } catch (e) {
    if (handleAuthError(e)) return
    error(String(e))
  }
}

// --- 监听器 ---
watch(activeProjectId, () => updateEditorReadonly())

watch(
  activeProject,
  async (project) => {
    if (currentMenu.value !== 'projects') return
    if (!project) return
    await nextTick()
    await initEditor()
    updateEditorReadonly()
  },
  { immediate: false }
)

watch(currentMenu, async (menu) => {
  if (menu !== 'projects') return
  await nextTick()
  await initEditor()
  await loadCompose()
})

watch(composeText, (value) => {
  if (!composeEditor) return
  if (composeEditor.getValue() === value) return
  composeEditor.setValue(value)
})

watch(logs, () => {
  syncLogEditor()
})

watch(actionLogs, () => {
  syncActionLogScroll()
})

// 防抖搜索 - 当关键字变化时自动重新加载
watch(debouncedContainerKeyword, () => {
  if (currentMenu.value === 'containers') {
    void loadContainers()
  }
})

watch(debouncedImageKeyword, () => {
  if (currentMenu.value === 'images') {
    void loadImages()
  }
})

// --- 生命周期 ---
onMounted(async () => {
  window.addEventListener('keydown', handleGlobalKeydown)
  if (!authed.value) return
  try {
    await bootstrapApp()
  } catch (e) {
    handleAuthError(e)
    error(String(e))
  }
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
  closeActionStream()
  closeLogStream()
  disposeEditor()
  disposeLogEditor()
})
</script>

<template>
  <div class="app-shell">
    <!-- Toast 通知 -->
    <TransitionGroup name="toast-list" tag="div" class="toast-container">
      <div
        v-for="toast in toasts"
        :key="toast.id"
        :class="['toast', `toast-${toast.type}`]"
        role="alert"
      >
        <span class="toast-message">{{ toast.message }}</span>
        <button class="toast-close" @click="removeToast(toast.id)" aria-label="关闭">
          <svg viewBox="0 0 24 24" width="14" height="14" stroke="currentColor" stroke-width="2" fill="none">
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
        </button>
      </div>
    </TransitionGroup>

    <header class="app-banner">
      <span>compose-ui</span>
      <button v-if="authed" class="logout-btn" @click="logout">退出登录</button>
    </header>

    <div v-if="authed" class="layout">
      <aside class="sidebar">
        <div class="menu-tabs">
          <button
            class="menu-btn"
            :class="{ active: currentMenu === 'projects' }"
            @click="switchMenu('projects')"
          >
            项目管理
          </button>
          <button
            class="menu-btn"
            :class="{ active: currentMenu === 'containers' }"
            @click="switchMenu('containers')"
          >
            容器管理
          </button>
          <button
            class="menu-btn"
            :class="{ active: currentMenu === 'images' }"
            @click="switchMenu('images')"
          >
            镜像管理
          </button>
        </div>

        <div class="header">
          <h2>{{ currentMenu === 'projects' ? 'Compose 项目' : currentMenu === 'containers' ? '容器列表' : '镜像列表' }}</h2>
          <button
            v-if="currentMenu === 'projects'"
            @click="loadProjects"
            :disabled="loading"
          >
            刷新
          </button>
          <button v-else-if="currentMenu === 'containers'" @click="loadContainers">刷新</button>
          <button v-else @click="loadImages" :disabled="imageLoading">刷新</button>
        </div>

        <ul v-if="currentMenu === 'projects'">
          <li
            v-for="project in projects"
            :key="project.id"
            :class="{ active: project.id === activeProjectId }"
            @click="chooseProject(project.id)"
          >
            <div class="name">{{ project.name }}</div>
            <div class="path">{{ project.composeFilePath || '未识别 compose 文件' }}</div>
          </li>
        </ul>

        <div v-else class="sidebar-note">
          {{
            currentMenu === 'containers'
              ? '在右侧容器列表中选择一个容器后可执行操作和查看日志。'
              : '通过右侧面板搜索、过滤并批量删除镜像。'
          }}
        </div>
      </aside>

      <main class="main">
        <template v-if="currentMenu === 'projects'">
          <section v-if="!activeProject" class="panel">
            <h3>项目管理</h3>
            <p class="msg">暂无可用 Compose 项目，请检查 Docker 容器和 Compose 标签。</p>
          </section>

          <template v-else>
            <section class="panel">
              <h3>Compose 编辑</h3>
              <div class="editor-shell" ref="editorHost"></div>
              <div class="actions">
                <button @click="loadCompose" :disabled="loading">重新读取</button>
                <button @click="formatCompose" :disabled="!activeProject.editable || loading">格式化</button>
                <button @click="saveCompose" :disabled="!activeProject.editable || loading">保存</button>
                <div class="actions-right">
                  <button @click="runProjectAction('start')" :disabled="actionRunning">启动组</button>
                  <button @click="runProjectAction('stop')" :disabled="actionRunning">停止组</button>
                  <button
                    @click="redeployProject"
                    :disabled="!activeProject.editable || actionRunning"
                  >
                    重部署
                  </button>
                </div>
              </div>
              <div v-if="actionType" class="action-log-panel">
                <div class="action-log-header">
                  <strong>操作日志 - {{ actionTypeLabel(actionType) }}</strong>
                  <span
                    class="tag"
                    :class="actionRunning ? 'tag-status-running' : 'tag-status-stopped'"
                  >
                    {{ actionRunning ? '执行中' : '已结束' }}
                  </span>
                </div>
                <pre ref="actionLogHost" class="action-log-content">{{
                  actionLogs || '等待日志输出...'
                }}</pre>
              </div>
            </section>

            <section class="panel">
              <h3>服务管理</h3>
              <table>
                <thead>
                  <tr>
                    <th>服务</th>
                    <th>镜像</th>
                    <th>状态</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="svc in activeProject.services"
                    :key="svc.id"
                    :class="{ activeRow: selectedService?.id === svc.id }"
                    @click="selectedService = svc"
                  >
                    <td>{{ svc.name }}</td>
                    <td><span class="tag tag-image">{{ svc.image }}</span></td>
                    <td>
                      <span
                        class="tag"
                        :class="containerStatusClass(svc.status)"
                      >
                        {{ containerStatusLabel(svc.status) }}
                      </span>
                    </td>
                    <td class="row-actions">
                      <button class="mini-btn" @click.stop="openServiceLogs(svc)">日志</button>
                      <button class="mini-btn" @click.stop="runServiceActionFor(svc, 'restart')">重启</button>
                      <button class="mini-btn" @click.stop="runServiceActionFor(svc, 'stop')">停止</button>
                      <button
                        class="mini-btn danger-btn"
                        @click.stop="runServiceActionFor(svc, 'delete')"
                      >
                        删除
                      </button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </section>
          </template>
        </template>

        <section v-if="currentMenu === 'containers'" class="panel">
          <h3>容器管理</h3>
          <div class="actions">
            <input
              v-model="containerKeyword"
              class="field"
              placeholder="搜索容器名称/镜像/项目 (自动搜索)"
            />
            <button @click="loadContainers">搜索</button>
          </div>
          <table>
            <thead>
              <tr>
                <th>容器</th>
                <th>所属项目</th>
                <th>镜像</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in containers"
                :key="item.id"
                :class="{ activeRow: selectedContainer?.id === item.id }"
                @click="chooseContainer(item)"
              >
                <td><span class="tag tag-container">{{ item.name }}</span></td>
                <td>
                  <button
                    v-if="item.project"
                    class="inline-link-btn"
                    @click.stop="jumpToProjectFromContainer(item)"
                  >
                    {{ item.project }}
                  </button>
                  <span v-else class="text-muted">未关联</span>
                </td>
                <td><span class="tag tag-image">{{ item.image }}</span></td>
                <td>
                  <span class="tag" :class="containerStatusClass(item.status)">
                    {{ containerStatusLabel(item.status) }}
                  </span>
                </td>
                <td class="row-actions">
                  <button class="mini-btn" @click.stop="openContainerLogs(item)">日志</button>
                  <button class="mini-btn" @click.stop="runContainerActionFor(item, 'restart')">重启</button>
                  <button class="mini-btn" @click.stop="runContainerActionFor(item, 'stop')">停止</button>
                  <button
                    class="mini-btn danger-btn"
                    @click.stop="runContainerActionFor(item, 'delete')"
                  >
                    删除
                  </button>
                </td>
              </tr>
              <tr v-if="!containers.length && !loading">
                <td colspan="5" class="empty-message">暂无容器</td>
              </tr>
            </tbody>
          </table>
        </section>

        <section v-if="currentMenu === 'images'" class="panel">
          <h3>镜像管理</h3>
          <div class="actions">
            <input
              v-model="imageKeyword"
              class="field"
              placeholder="搜索镜像 ID 或标签 (自动搜索)"
            />
            <select v-model="imageUsedFilter" class="field" @change="loadImages">
              <option value="all">全部</option>
              <option value="used">仅使用中</option>
              <option value="unused">仅未使用</option>
            </select>
            <button @click="loadImages" :disabled="imageLoading">查询</button>
            <button
              class="danger-btn"
              @click="deleteAllDanglingImages"
              :disabled="imageLoading"
            >
              删除所有空镜像
            </button>
            <button
              class="danger-btn"
              @click="deleteAllUnusedImages"
              :disabled="imageLoading"
            >
              删除所有未使用镜像
            </button>
            <button @click="deleteSelectedImages" :disabled="selectedImageIds.length === 0">
              批量删除
            </button>
          </div>
          <table>
            <thead>
              <tr>
                <th>
                  <input
                    type="checkbox"
                    :checked="allImagesSelected"
                    @change="onToggleSelectAllImages"
                  />
                </th>
                <th>镜像标签</th>
                <th>大小</th>
                <th>创建时间</th>
                <th>使用状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="img in images" :key="img.id">
                <td>
                  <input
                    type="checkbox"
                    :checked="selectedImageIds.includes(img.id)"
                    @change="(event) => onToggleImage(event, img.id)"
                  />
                </td>
                <td>
                  <div class="tag-list">
                    <span
                      v-for="tag in img.repoTags"
                      :key="`${img.id}-${tag}`"
                      class="tag tag-image"
                    >
                      {{ tag }}
                    </span>
                  </div>
                </td>
                <td><span class="tag tag-size">{{ formatBytes(img.size) }}</span></td>
                <td>{{ formatCreated(img.created) }}</td>
                <td>
                  <span class="tag" :class="img.used ? 'tag-used' : 'tag-unused'">
                    {{ img.used ? '使用中' : '未使用' }}
                  </span>
                </td>
              </tr>
              <tr v-if="!images.length && !imageLoading">
                <td colspan="5" class="empty-message">暂无镜像</td>
              </tr>
            </tbody>
          </table>
        </section>

        <Transition name="drawer-fade">
          <div v-if="showLogDrawer" class="drawer-mask" @click="closeLogDrawer"></div>
        </Transition>
        <Transition name="drawer-slide">
          <aside v-if="showLogDrawer" class="log-drawer" @click.stop>
            <div class="drawer-header">
              <h3>
                <span>日志 - {{ getActiveLogTarget()?.name }}</span>
                <span
                  class="stream-status"
                  :class="{
                    'status-idle': logStream.status.value === 'idle',
                    'status-connecting': logStream.status.value === 'connecting',
                    'status-connected': logStream.status.value === 'connected',
                    'status-disconnected': logStream.status.value === 'disconnected',
                    'status-error': logStream.status.value === 'error',
                  }"
                  :title="`流状态：${logStream.status.value}`"
                >
                  <span class="status-dot"></span>
                  <span class="status-text">{{
                    logStream.status.value === 'connected' ? '实时输出中' :
                    logStream.status.value === 'connecting' ? '连接中...' :
                    logStream.status.value === 'error' ? '连接错误' : '已停止'
                  }}</span>
                </span>
              </h3>
              <div class="drawer-header-actions">
                <label class="stream-toggle" :title="logStream.status.value">
                  <span class="stream-toggle-label">实时</span>
                  <button
                    class="stream-toggle-btn"
                    :class="{ active: logStream.active.value }"
                    @click="logStream.active.value ? closeLogStream() : openLogStream()"
                    :disabled="!getActiveLogTarget()"
                  >
                    <span class="stream-toggle-slider"></span>
                  </button>
                </label>
                <button class="drawer-close" @click="closeLogDrawer" title="关闭">
                  <svg viewBox="0 0 24 24" width="18" height="18" stroke="currentColor" stroke-width="2" fill="none">
                    <line x1="18" y1="6" x2="6" y2="18"></line>
                    <line x1="6" y1="6" x2="18" y2="18"></line>
                  </svg>
                </button>
              </div>
            </div>
            <div class="actions">
              <button class="follow-toggle" @click="autoFollowLogs = !autoFollowLogs">
                {{ autoFollowLogs ? '关闭自动跟随' : '开启自动跟随' }}
              </button>
            </div>
            <div class="log-editor-shell" ref="logEditorHost"></div>
          </aside>
        </Transition>
      </main>
    </div>

    <div v-else class="login-wrap">
      <section class="login-card">
        <h2>登录 compose ui</h2>
        <p class="msg">docker-compose 可视化管理工具</p>
        <form class="login-form" @submit.prevent="submitLogin">
          <input
            v-model="loginUser"
            class="field"
            placeholder="用户名"
            autocomplete="username"
          />
          <input
            v-model="loginPass"
            class="field"
            type="password"
            placeholder="密码"
            autocomplete="current-password"
          />
          <button type="submit" :disabled="authLoading">
            {{ authLoading ? '登录中...' : '登录' }}
          </button>
        </form>
        <p v-if="authError" class="login-error" role="alert">{{ authError }}</p>
      </section>
    </div>
  </div>
</template>

<style>
/* Toast 样式 */
.toast-container {
  position: fixed;
  top: 16px;
  right: 16px;
  z-index: 100;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 400px;
}

.toast {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 14px;
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
  border: 1px solid #e2e8f0;
  min-width: 280px;
  animation: slide-in 0.2s ease;
}

@keyframes slide-in {
  from {
    transform: translateX(100%);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}

.toast-success { border-left: 4px solid #22c55e; }
.toast-error { border-left: 4px solid #ef4444; }
.toast-info { border-left: 4px solid #3b82f6; }
.toast-warning { border-left: 4px solid #f59e0b; }

.toast-message {
  flex: 1;
  font-size: 14px;
  color: #1a1b25;
  word-break: break-word;
}

.toast-close {
  background: transparent;
  padding: 4px;
  color: #9ca3af;
  cursor: pointer;
  border: none;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.toast-close:hover {
  background: #f3f4f6;
  color: #1a1b25;
}

.toast-list-enter-active,
.toast-list-leave-active {
  transition: all 0.25s ease;
}

.toast-list-enter-from {
  transform: translateX(100%);
  opacity: 0;
}

.toast-list-leave-to {
  transform: translateX(100%);
  opacity: 0;
}

/* 流状态指示器 */
.stream-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  padding: 4px 10px;
  border-radius: 999px;
  margin-left: 10px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #9ca3af;
}

.status-idle { background: #f3f4f6; }
.status-idle .status-dot { background: #9ca3af; }

.status-connecting {
  background: #fef3c7;
}
.status-connecting .status-dot {
  background: #f59e0b;
  animation: pulse 1s infinite;
}

.status-connected {
  background: #d1fae5;
}
.status-connected .status-dot { background: #10b981; }

.status-disconnected { background: #f3f4f6; }
.status-disconnected .status-dot { background: #6b7280; }

.status-error {
  background: #fee2e2;
}
.status-error .status-dot { background: #ef4444; }

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

.status-text {
  font-size: 11px;
  font-weight: 500;
  color: #374151;
}

/* 其他样式保留 */
.empty-message {
  text-align: center;
  color: #9ca3af;
  padding: 24px;
}

.text-muted {
  color: #9ca3af;
}
</style>
