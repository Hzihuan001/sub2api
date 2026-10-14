import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { NavigationGuard, RouteLocationNormalized, Router, RouterOptions } from 'vue-router'
import type { CustomFeatureRegistry } from '../registry'

const routerHarness = vi.hoisted(() => ({ guard: null as NavigationGuard | null }))
const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: true,
  isAdmin: false,
  isOperator: false,
  isManagement: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
  can: vi.fn((permission: string) => permission === '__none__'),
  canOperator: vi.fn((permission: string) => permission === '__none__'),
  ensureOperatorRolePolicy: vi.fn().mockResolvedValue(undefined),
}))
const appStore = vi.hoisted(() => ({
  siteName: 'Host integration test',
  siteLogo: '',
  siteVersion: 'test',
  sidebarCollapsed: false,
  mobileOpen: false,
  sidebarScrollTop: 0,
  backendModeEnabled: false,
  publicSettingsLoaded: true,
  cachedPublicSettings: { custom_menu_items: [] },
  fetchPublicSettings: vi.fn().mockResolvedValue(undefined),
  toggleSidebar: vi.fn(),
  setMobileOpen: vi.fn(),
}))
const adminSettingsStore = vi.hoisted(() => ({
  customMenuItems: [],
  opsMonitoringEnabled: true,
  paymentEnabled: true,
  fetch: vi.fn().mockResolvedValue(undefined),
  fetchOpsCapabilities: vi.fn().mockResolvedValue(undefined),
}))
const onboardingStore = vi.hoisted(() => ({
  isCurrentStep: vi.fn().mockReturnValue(false),
  nextStep: vi.fn(),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => authStore,
  useAppStore: () => appStore,
  useAdminSettingsStore: () => adminSettingsStore,
  useOnboardingStore: () => onboardingStore,
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => authStore }))
vi.mock('@/stores/app', () => ({ useAppStore: () => appStore }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => adminSettingsStore }))
vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: () => ({
    initialized: true,
    fetchStatus: vi.fn(),
    requireAcknowledgement: vi.fn(),
  }),
}))
vi.mock('@/api/setup', () => ({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) }))
vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))
vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => `translated:${key}` }) }
})
vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    createWebHistory: (base?: string) => actual.createMemoryHistory(base),
    createRouter: (options: RouterOptions) => {
      // Retain the real matcher/router and registration. Capture the actual host
      // guard so denied direct URLs do not load unrelated lazy page components.
      const router = actual.createRouter(options)
      const registerGuard = router.beforeEach.bind(router)
      router.beforeEach = (guard) => {
        routerHarness.guard = guard
        return registerGuard(guard)
      }
      return router
    },
  }
})

const featureID = 'host-navigation-test-probe'
const userPath = '/__host-navigation-probe'
const permissionPath = '/admin/__host-navigation-probe/permission'
const adminPath = '/admin/__host-navigation-probe/admin'
const probeView = defineComponent({ render: () => h('div', 'probe view') })
const probeIcon = defineComponent({ render: () => h('span', { 'aria-hidden': 'true' }) })

let registry: CustomFeatureRegistry
const mounted: VueWrapper[] = []

function registerProbe(): void {
  registry.register({
    id: featureID,
    routes: [
      { path: userPath, name: 'HostProbeUser', component: probeView, meta: { requiresAuth: true, title: 'Host probe' } },
      { path: permissionPath, name: 'HostProbePermission', component: probeView, meta: { requiresAuth: true, requiredPermission: 'users', title: 'Host permission probe' } },
      { path: adminPath, name: 'HostProbeAdmin', component: probeView, meta: { requiresAuth: true, requiresAdmin: true, title: 'Host admin probe' } },
    ],
    menuItems: [
      { id: 'host-probe-user', path: userPath, labelKey: 'hostProbe.user', icon: probeIcon, hideInSimpleMode: true },
      { id: 'host-probe-permission', path: permissionPath, labelKey: 'hostProbe.permission', icon: probeIcon, permission: 'users' },
      { id: 'host-probe-admin', path: adminPath, labelKey: 'hostProbe.admin', icon: probeIcon, adminOnly: true, permission: 'users' },
    ],
  })
}

function setRole(role: 'admin' | 'operator' | 'user', permissions: string[] = []): void {
  authStore.isAdmin = role === 'admin'
  authStore.isOperator = role === 'operator'
  authStore.isManagement = role !== 'user'
  authStore.can.mockImplementation((permission) => role === 'admin' || permissions.includes(permission))
  authStore.canOperator.mockImplementation((permission) => role === 'admin' || permissions.includes(permission))
}

async function mountSidebar(): Promise<VueWrapper> {
  const { createRouter, createMemoryHistory } = await import('vue-router')
  // The sidebar uses a real RouterLink/router plugin; its menu contributions
  // still come from the application's real registry and host adapter.
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: probeView }],
  })
  await router.push('/host-sidebar-fixture')
  await router.isReady()
  const { default: AppSidebar } = await import('@/components/layout/AppSidebar.vue')
  const wrapper = mount(AppSidebar, {
    global: { plugins: [router], stubs: { VersionBadge: true, Icon: true } },
  })
  mounted.push(wrapper)
  await flushPromises()
  return wrapper
}

function assertProbeMenu(wrapper: VueWrapper): void {
  const link = wrapper.find(`a[href="${userPath}"]`)
  if (!link.exists() || link.text() !== 'translated:hostProbe.user') {
    throw new Error('Host sidebar did not render the registered probe path and label')
  }
}

function assertProbeRoute(router: Router): void {
  const route = router.getRoutes().find((item) => item.name === 'HostProbeUser')
  if (route?.path !== userPath || route.meta.requiresAuth !== true) {
    throw new Error('Host router did not register the authenticated probe route')
  }
}

async function runHostGuard(router: Router, path: string) {
  const guard = routerHarness.guard
  if (!guard) throw new Error('The application beforeEach guard was not registered')
  const next = vi.fn()
  // Resolve the real route and its real merged meta, rather than supplying
  // hand-written authorization metadata that could hide a lost registration.
  const to = router.resolve(path) as RouteLocationNormalized
  const from = router.resolve('/dashboard') as RouteLocationNormalized
  await guard.call(undefined, to, from, next)
  return next
}

beforeEach(async () => {
  vi.resetModules()
  vi.clearAllMocks()
  routerHarness.guard = null
  authStore.isAuthenticated = true
  authStore.isSimpleMode = false
  appStore.sidebarScrollTop = 0
  setRole('user')
  registry = (await import('@/custom')).customFeatureRegistry
})

afterEach(() => {
  mounted.splice(0).forEach((wrapper) => wrapper.unmount())
  registry.unregister(featureID)
  // Never clear the singleton: the host's shipped features must remain intact.
  expect(registry.has('image-studio')).toBe(true)
  expect(registry.has('operator')).toBe(true)
})

describe('custom feature real host navigation', () => {
  it('renders registered user path and translated label through the real sidebar', async () => {
    registerProbe()
    const wrapper = await mountSidebar()
    assertProbeMenu(wrapper)
    expect(wrapper.find(`a[href="${permissionPath}"]`).exists()).toBe(false)
    expect(wrapper.find(`a[href="${adminPath}"]`).exists()).toBe(false)
  })

  it('hides the registered user contribution in simple mode', async () => {
    registerProbe()
    authStore.isSimpleMode = true
    const wrapper = await mountSidebar()
    expect(wrapper.find(`a[href="${userPath}"]`).exists()).toBe(false)
    expect(wrapper.find('a[href="/image-studio"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/keys"]').exists()).toBe(true)
  })

  it.each([false, true])('honors operator permission granted=%s without overriding adminOnly on the same menu entry', async (granted) => {
    registerProbe()
    setRole('operator', granted ? ['users'] : [])
    const wrapper = await mountSidebar()
    expect(wrapper.find(`a[href="${permissionPath}"]`).exists()).toBe(granted)
    if (granted) expect(wrapper.get(`a[href="${permissionPath}"]`).text()).toBe('translated:hostProbe.permission')
    expect(wrapper.find(`a[href="${adminPath}"]`).exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/roles"]').exists()).toBe(false)
  })

  it('renders permitted and admin-only contributions plus the real role entry for an admin', async () => {
    registerProbe()
    setRole('admin')
    const wrapper = await mountSidebar()
    assertProbeMenu(wrapper)
    expect(wrapper.get(`a[href="${permissionPath}"]`).text()).toBe('translated:hostProbe.permission')
    expect(wrapper.get(`a[href="${adminPath}"]`).text()).toBe('translated:hostProbe.admin')
    expect(wrapper.get('a[href="/admin/roles"]').text()).toBe('translated:nav.rolePermissions')
  })

  it('keeps roles directly after users and usage directly before audit logs in the real admin menu', async () => {
    setRole('admin')
    const wrapper = await mountSidebar()
    const adminSection = wrapper.get('nav .sidebar-section')
    const users = adminSection.get('a[href="/admin/users"]')
    const roles = adminSection.get('a[href="/admin/roles"]')
    const usage = adminSection.get('a[href="/admin/usage"]')
    const auditLogs = adminSection.get('a[href="/admin/audit-logs"]')
    expect(roles.element.previousElementSibling).toBe(users.element)
    expect(usage.element.nextElementSibling).toBe(auditLogs.element)
    expect(roles.element.parentElement).toBe(adminSection.element)
    expect(usage.element.parentElement).toBe(adminSection.element)
  })

  it('renders prompt audit only inside the expandable security audit group', async () => {
    setRole('admin')
    const wrapper = await mountSidebar()
    const adminSection = wrapper.get('nav .sidebar-section')
    const securityGroup = adminSection.findAll('button').find((button) => button.text() === 'translated:nav.securityAudit')
    if (!securityGroup) throw new Error('The real security audit group is missing')
    expect(wrapper.find('a[href="/admin/prompt-audit"]').exists()).toBe(false)
    await securityGroup.trigger('click')
    const promptAudit = adminSection.get('a[href="/admin/prompt-audit"]')
    expect(promptAudit.text()).toBe('translated:nav.promptAudit')
    expect(promptAudit.element.parentElement).toBe(securityGroup.element.nextElementSibling)
    expect(promptAudit.element.parentElement).not.toBe(adminSection.element)
    expect(wrapper.findAll('a[href="/admin/prompt-audit"]')).toHaveLength(1)
    await securityGroup.trigger('click')
    expect(wrapper.find('a[href="/admin/prompt-audit"]').exists()).toBe(false)
  })

  it('registers custom routes in the actual application router before catch-all matching', async () => {
    registerProbe()
    const { default: router } = await import('@/router')
    assertProbeRoute(router)
    expect(router.resolve(userPath).name).toBe('HostProbeUser')
    expect(router.resolve(permissionPath).meta.requiredPermission).toBe('users')
    expect(router.resolve(adminPath).meta.requiresAdmin).toBe(true)
    expect(router.getRoutes().filter((route) => route.path === userPath)).toHaveLength(1)
    expect(router.resolve('/image-studio').name).toBe('ImageStudio')
    expect(router.resolve('/admin/roles').meta.requiresAdmin).toBe(true)
    expect(router.resolve('/admin/roles').name).toBe('AdminRolePermissions')
    expect(router.getRoutes().filter((route) => route.path === '/admin/roles')).toHaveLength(1)
    expect(router.getRoutes().filter((route) => route.name === 'AdminRolePermissions')).toHaveLength(1)
  })

  it('redirects an unauthenticated direct probe URL to login with the intended destination', async () => {
    registerProbe()
    const { default: router } = await import('@/router')
    authStore.isAuthenticated = false
    const next = await runHostGuard(router, userPath)
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith({ path: '/login', query: { redirect: userPath } })
  })

  it.each([
    { role: 'user' as const, permissions: [] as string[], allowed: false },
    { role: 'operator' as const, permissions: [] as string[], allowed: false },
    { role: 'operator' as const, permissions: ['users'], allowed: true },
    { role: 'admin' as const, permissions: [] as string[], allowed: true },
  ])('applies the real permission guard to a direct URL for $role with $permissions', async ({ role, permissions, allowed }) => {
    registerProbe()
    setRole(role, permissions)
    const { default: router } = await import('@/router')
    const next = await runHostGuard(router, permissionPath)
    expect(next).toHaveBeenCalledOnce()
    if (allowed) expect(next).toHaveBeenCalledWith()
    else expect(next).toHaveBeenCalledWith('/dashboard')
    if (role === 'operator') expect(authStore.ensureOperatorRolePolicy).toHaveBeenCalledOnce()
  })

  it.each(['user', 'operator', 'admin'] as const)('enforces admin-only direct URLs for %s even with the general permission granted', async (role) => {
    registerProbe()
    setRole(role, ['users'])
    const { default: router } = await import('@/router')
    for (const path of [adminPath, '/admin/roles']) {
      const next = await runHostGuard(router, path)
      expect(next).toHaveBeenCalledOnce()
      if (role === 'admin') expect(next).toHaveBeenCalledWith()
      else expect(next).toHaveBeenCalledWith(role === 'operator' ? '/admin/users' : '/dashboard')
    }
  })

  it('fails the same host contracts when the probe feature is not registered', async () => {
    const { default: router } = await import('@/router')
    const wrapper = await mountSidebar()
    expect(registry.has(featureID)).toBe(false)
    expect(router.getRoutes().some((route) => route.name === 'HostProbeUser')).toBe(false)
    expect(wrapper.find(`a[href="${userPath}"]`).exists()).toBe(false)
    expect(() => assertProbeRoute(router)).toThrow('Host router did not register')
    expect(() => assertProbeMenu(wrapper)).toThrow('Host sidebar did not render')
  })
})
