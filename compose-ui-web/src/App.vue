<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import loader from '@monaco-editor/loader'
import * as monaco from 'monaco-editor'
import * as prettier from 'prettier/standalone'
import prettierPluginEstree from 'prettier/plugins/estree'
import prettierPluginYaml from 'prettier/plugins/yaml'
import type { editor } from 'monaco-editor'
import {
  deleteImages,
  getComposeFile,
  getLogs,
  listContainers,
  listImages,
  listProjects,
  logsStreamUrl,
  projectAction,
  redeploy,
  saveComposeFile,
  serviceAction,
} from './api'
import type { ContainerItem, ImageItem, Project, Service } from './types'

loader.config({ monaco })

const projects = ref<Project[]>([])
const loading = ref(false)
const currentMenu = ref<'projects' | 'containers' | 'images'>('projects')
const activeProjectId = ref('')
const composeText = ref('')
const composeMtime = ref(0)
const message = ref('')
const selectedService = ref<Service | null>(null)
const selectedContainer = ref<ContainerItem | null>(null)
const logs = ref('')
const containers = ref<ContainerItem[]>([])
const containerKeyword = ref('')
const images = ref<ImageItem[]>([])
const imageKeyword = ref('')
const imageUsedFilter = ref<'all' | 'used' | 'unused'>('all')
const selectedImageIds = ref<string[]>([])
const imageLoading = ref(false)
const showLogDrawer = ref(false)
const autoFollowLogs = ref(true)
const editorHost = ref<HTMLElement | null>(null)
const logEditorHost = ref<HTMLElement | null>(null)
let composeEditor: editor.IStandaloneCodeEditor | null = null
let editorInitPromise: Promise<void> | null = null
let logEditor: editor.IStandaloneCodeEditor | null = null
let logEditorInitPromise: Promise<void> | null = null
let eventSource: EventSource | null = null

const activeProject = computed(() => {
  const items = Array.isArray(projects.value) ? projects.value : []
  return items.find((p) => p.id === activeProjectId.value) ?? null
})

const allImagesSelected = computed(() => images.value.length > 0 && selectedImageIds.value.length === images.value.length)

function switchMenu(menu: 'projects' | 'containers' | 'images') {
  currentMenu.value = menu
  closeLogDrawer()
  if (menu !== 'projects') disposeEditor()
  if (menu === 'images') void loadImages()
  if (menu === 'containers') void loadContainers()
  if (menu === 'projects') void loadProjects()
}

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
      theme: 'vs',
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

async function loadProjects() {
  loading.value = true
  try {
    const items = await listProjects()
    projects.value = Array.isArray(items) ? items : []
    if (!activeProjectId.value && projects.value.length > 0) {
      activeProjectId.value = projects.value[0].id
    }
    message.value = `已加载 ${projects.value.length} 个项目`
  } catch (e) {
    message.value = String(e)
  } finally {
    loading.value = false
  }
}

async function loadContainers() {
  try {
    const items = await listContainers(containerKeyword.value)
    containers.value = items
    if (selectedContainer.value) {
      selectedContainer.value = items.find((item) => item.id === selectedContainer.value?.id) ?? null
    }
  } catch (e) {
    message.value = String(e)
  }
}

async function loadImages() {
  imageLoading.value = true
  try {
    images.value = await listImages(imageKeyword.value, imageUsedFilter.value)
    const current = new Set(images.value.map((i) => i.id))
    selectedImageIds.value = selectedImageIds.value.filter((id) => current.has(id))
  } catch (e) {
    message.value = String(e)
  } finally {
    imageLoading.value = false
  }
}

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
    message.value = '请先选择要删除的镜像'
    return
  }
  const confirmed = window.confirm(`确认删除选中的 ${selectedImageIds.value.length} 个镜像吗？`)
  if (!confirmed) return
  try {
    const results = await deleteImages(selectedImageIds.value, false)
    const failed = results.filter((r) => !r.success)
    if (failed.length === 0) {
      message.value = `已删除 ${results.length} 个镜像`
    } else {
      message.value = `删除完成，失败 ${failed.length} 个`
    }
    await loadImages()
  } catch (e) {
    message.value = String(e)
  }
}

async function deleteAllDanglingImages() {
  try {
    const all = await listImages('', 'all')
    const target = all.filter((img) => img.repoTags.some((tag) => tag === '<none>:<none>')).map((img) => img.id)
    if (target.length === 0) {
      message.value = '没有可删除的空镜像'
      return
    }
    const confirmed = window.confirm(`确认删除所有空镜像吗？共 ${target.length} 个`)
    if (!confirmed) return
    const results = await deleteImages(target, false)
    const failed = results.filter((r) => !r.success).length
    message.value = failed === 0 ? `已删除空镜像 ${results.length} 个` : `空镜像删除完成，失败 ${failed} 个`
    await loadImages()
  } catch (e) {
    message.value = String(e)
  }
}

async function deleteAllUnusedImages() {
  try {
    const unused = await listImages('', 'unused')
    const target = unused.map((img) => img.id)
    if (target.length === 0) {
      message.value = '没有可删除的未使用镜像'
      return
    }
    const confirmed = window.confirm(`确认删除所有未使用镜像吗？共 ${target.length} 个`)
    if (!confirmed) return
    const results = await deleteImages(target, false)
    const failed = results.filter((r) => !r.success).length
    message.value = failed === 0 ? `已删除未使用镜像 ${results.length} 个` : `未使用镜像删除完成，失败 ${failed} 个`
    await loadImages()
  } catch (e) {
    message.value = String(e)
  }
}

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
    message.value = 'compose 文件已加载'
  } catch (e) {
    message.value = String(e)
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
    message.value = 'compose 格式化完成'
  } catch (e) {
    message.value = `格式化失败: ${String(e)}`
  }
}

async function saveCompose() {
  if (!activeProject.value || !activeProject.value.editable) return
  try {
    const file = await saveComposeFile(activeProject.value.id, getComposeContent(), composeMtime.value)
    composeMtime.value = file.mtime
    message.value = file.backupPath ? `保存成功，备份：${file.backupPath}` : '保存成功'
  } catch (e) {
    message.value = String(e)
  }
}

async function redeployProject() {
  if (!activeProject.value) return
  try {
    const res = await redeploy(activeProject.value.id)
    message.value = `${res.message} (${res.durationMs}ms)`
    await loadProjects()
    await loadContainers()
    await loadImages()
  } catch (e) {
    message.value = String(e)
  }
}

async function runProjectAction(action: 'start' | 'stop') {
  if (!activeProject.value) return
  try {
    const res = await projectAction(activeProject.value.id, action)
    message.value = `${action} 完成: ${res.message}`
    await loadProjects()
    await loadContainers()
    await loadImages()
  } catch (e) {
    message.value = String(e)
  }
}

async function runServiceAction(action: 'stop' | 'restart' | 'delete') {
  if (!selectedService.value) return
  try {
    const res = await serviceAction(selectedService.value.id, action)
    message.value = `${selectedService.value.name} ${action} 完成: ${res.message}`
    await loadProjects()
    await loadContainers()
    await loadImages()
  } catch (e) {
    message.value = String(e)
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
    message.value = `${selectedContainer.value.name} ${action} 完成: ${res.message}`
    await loadProjects()
    await loadContainers()
    await loadImages()
  } catch (e) {
    message.value = String(e)
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
    logs.value = String(e)
  }
}

function openLogStream() {
  closeLogStream()
  const target = getActiveLogTarget()
  if (!target) return
  eventSource = new EventSource(logsStreamUrl(target.containerId))
  eventSource.addEventListener('log', (evt) => {
    const msg = (evt as MessageEvent<string>).data
    logs.value += `\n${msg}`
  })
  eventSource.onerror = () => {
    message.value = '日志流连接断开，可手动重连'
    closeLogStream()
  }
}

function closeLogStream() {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
}

async function openLogDrawer() {
  if (!getActiveLogTarget()) {
    message.value = currentMenu.value === 'containers' ? '请先在容器列表中选择容器' : '请先在列表中选择服务'
    return
  }
  showLogDrawer.value = true
  await nextTick()
  await initLogEditor()
  await loadLogs()
  syncLogEditor()
}

function closeLogDrawer() {
  showLogDrawer.value = false
  closeLogStream()
  disposeLogEditor()
}

function handleGlobalKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (!showLogDrawer.value) return
  closeLogDrawer()
}

function chooseProject(id: string) {
  activeProjectId.value = id
  selectedService.value = null
  logs.value = ''
  showLogDrawer.value = false
  closeLogStream()
  void loadCompose()
}

function chooseContainer(item: ContainerItem) {
  selectedContainer.value = item
}

async function jumpToProjectFromContainer(item: ContainerItem) {
  const projectName = item.project.trim()
  if (!projectName) {
    message.value = '该容器未关联 Compose 项目'
    return
  }
  try {
    await loadProjects()
    const target =
      projects.value.find((p) => p.name === projectName) ??
      projects.value.find((p) => p.name.toLowerCase() === projectName.toLowerCase())
    if (!target) {
      message.value = `未找到项目：${projectName}`
      return
    }
    currentMenu.value = 'projects'
    chooseProject(target.id)
    message.value = `已跳转到项目：${target.name}`
  } catch (e) {
    message.value = String(e)
  }
}

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
  if (menu !== 'projects') {
    return
  }
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

onMounted(async () => {
  window.addEventListener('keydown', handleGlobalKeydown)
  await loadProjects()
  await loadContainers()
  await loadImages()
  await nextTick()
  await initEditor()
  await loadCompose()
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
  closeLogStream()
  disposeEditor()
  disposeLogEditor()
})
</script>

<template>
  <div class="app-shell">
    <header class="app-banner">compose-ui</header>
    <div class="layout">
      <aside class="sidebar">
      <div class="menu-tabs">
        <button class="menu-btn" :class="{ active: currentMenu === 'projects' }" @click="switchMenu('projects')">
          项目管理
        </button>
        <button class="menu-btn" :class="{ active: currentMenu === 'containers' }" @click="switchMenu('containers')">
          容器管理
        </button>
        <button class="menu-btn" :class="{ active: currentMenu === 'images' }" @click="switchMenu('images')">
          镜像管理
        </button>
      </div>
      <div class="header">
        <h2>{{ currentMenu === 'projects' ? 'Compose 项目' : currentMenu === 'containers' ? '容器列表' : '镜像列表' }}</h2>
        <button v-if="currentMenu === 'projects'" @click="loadProjects" :disabled="loading">刷新</button>
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
        {{ currentMenu === 'containers' ? '在右侧容器列表中选择一个容器后可执行操作和查看日志。' : '通过右侧面板搜索、过滤并批量删除镜像。' }}
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
          <button @click="loadCompose">重新读取</button>
          <button @click="formatCompose" :disabled="!activeProject.editable">格式化</button>
          <button @click="saveCompose" :disabled="!activeProject.editable">保存</button>
          <div class="actions-right">
            <button @click="runProjectAction('start')">启动组</button>
            <button @click="runProjectAction('stop')">停止组</button>
            <button @click="redeployProject" :disabled="!activeProject.editable">重部署</button>
          </div>
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
                <span class="tag" :class="containerStatusClass(svc.status)">
                  {{ containerStatusLabel(svc.status) }}
                </span>
              </td>
              <td class="row-actions">
                <button class="mini-btn" @click.stop="openServiceLogs(svc)">日志</button>
                <button class="mini-btn" @click.stop="runServiceActionFor(svc, 'restart')">重启</button>
                <button class="mini-btn" @click.stop="runServiceActionFor(svc, 'stop')">停止</button>
                <button class="mini-btn danger-btn" @click.stop="runServiceActionFor(svc, 'delete')">删除</button>
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
          <input v-model="containerKeyword" class="field" placeholder="搜索容器名称/镜像/项目" />
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
              <td>{{ item.name }}</td>
              <td>
                <button class="inline-link-btn" @click.stop="jumpToProjectFromContainer(item)">
                  {{ item.project || '未关联' }}
                </button>
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
                <button class="mini-btn danger-btn" @click.stop="runContainerActionFor(item, 'delete')">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section v-if="currentMenu === 'images'" class="panel">
        <h3>镜像管理</h3>
        <div class="actions">
          <input v-model="imageKeyword" class="field" placeholder="搜索镜像ID或标签" />
          <select v-model="imageUsedFilter" class="field">
            <option value="all">全部</option>
            <option value="used">仅使用中</option>
            <option value="unused">仅未使用</option>
          </select>
          <button @click="loadImages" :disabled="imageLoading">查询</button>
          <button class="danger-btn" @click="deleteAllDanglingImages" :disabled="imageLoading">删除所有空镜像</button>
          <button class="danger-btn" @click="deleteAllUnusedImages" :disabled="imageLoading">删除所有未使用镜像</button>
          <button @click="deleteSelectedImages" :disabled="selectedImageIds.length === 0">批量删除</button>
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
                  <span v-for="tag in img.repoTags" :key="`${img.id}-${tag}`" class="tag tag-image">{{ tag }}</span>
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
          </tbody>
        </table>
      </section>

      <footer class="msg">{{ message }}</footer>

      <Transition name="drawer-fade">
        <div v-if="showLogDrawer" class="drawer-mask" @click="closeLogDrawer"></div>
      </Transition>
      <Transition name="drawer-slide">
        <aside v-if="showLogDrawer" class="log-drawer" @click.stop>
          <div class="drawer-header">
            <h3>日志 - {{ getActiveLogTarget()?.name }}</h3>
            <button class="drawer-close" @click="closeLogDrawer">关闭</button>
          </div>
          <div class="actions">
            <button @click="loadLogs" :disabled="!getActiveLogTarget()">读取历史</button>
            <button @click="openLogStream" :disabled="!getActiveLogTarget()">开启实时流</button>
            <button @click="closeLogStream">关闭实时流</button>
            <button class="follow-toggle" @click="autoFollowLogs = !autoFollowLogs">
              {{ autoFollowLogs ? '关闭自动跟随' : '开启自动跟随' }}
            </button>
          </div>
          <div class="log-editor-shell" ref="logEditorHost"></div>
        </aside>
      </Transition>
      </main>
    </div>
  </div>
</template>
