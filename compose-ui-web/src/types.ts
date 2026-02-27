export type Service = {
  id: string
  name: string
  containerId: string
  status: string
  image: string
}

export type Project = {
  id: string
  name: string
  composeFilePath?: string
  workingDir?: string
  editable: boolean
  services: Service[]
}

export type ComposeFile = {
  content: string
  mtime: number
  size: number
  backupPath?: string
}

export type ActionResult = {
  success: boolean
  message: string
  stdout?: string
  stderr?: string
  durationMs: number
}

export type ApiError = {
  code: string
  message: string
  detail?: string
  retryable: boolean
}

export type ContainerItem = {
  id: string
  name: string
  image: string
  status: string
  project: string
}

export type ImageItem = {
  id: string
  repoTags: string[]
  size: number
  created: number
  used: boolean
}

export type ImageDeleteResult = {
  imageId: string
  success: boolean
  message: string
}
