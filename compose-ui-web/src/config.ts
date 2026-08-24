// 运行时配置。由后端在部署时注入到 window.__COMPOSE_UI_CONFIG__，
// 也可通过构建环境变量 VITE_ENABLE_PROJECT_MANAGEMENT 覆盖。
export const enableProjectManagement: boolean = (() => {
  try {
    const win = window as unknown as { __COMPOSE_UI_CONFIG__?: { enableProjectManagement?: boolean } }
    if (win.__COMPOSE_UI_CONFIG__ && typeof win.__COMPOSE_UI_CONFIG__.enableProjectManagement === 'boolean') {
      return win.__COMPOSE_UI_CONFIG__.enableProjectManagement
    }
  } catch {
    // ignore
  }
  const fromEnv = import.meta.env.VITE_ENABLE_PROJECT_MANAGEMENT as string | undefined
  if (fromEnv !== undefined && fromEnv !== '') {
    return fromEnv === 'true' || fromEnv === '1'
  }
  return false
})()
