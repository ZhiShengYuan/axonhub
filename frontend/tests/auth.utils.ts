import { expect, Page } from '@playwright/test'

// Type declaration for process
declare const process: {
  env: Record<string, string | undefined>
}

export interface AdminCredentials {
  email: string
  password: string
}

const defaultCredentials: AdminCredentials = {
  email: process.env.AXONHUB_ADMIN_EMAIL || 'my@example.com',
  password: process.env.AXONHUB_ADMIN_PASSWORD || 'pwd123456',
}
const API_BASE_URL = process.env.AXONHUB_API_URL || 'http://localhost:8099'
const ACCESS_TOKEN_KEY = 'axonhub_access_token'
const USER_INFO_KEY = 'axonhub_user_info'

async function seedAuthToken(
  page: Page,
  credentials: AdminCredentials = defaultCredentials
): Promise<void> {
  const result = await page.evaluate(
    async ({ email, password, tokenKey, userKey, onboardMutation }) => {
      const res = await fetch(`/admin/auth/signin`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(`signin failed: ${res.status()} ${text}`)
      }
      const data = (await res.json()) as { user: unknown; token: string }
      localStorage.setItem(tokenKey, data.token)
      localStorage.setItem(userKey, JSON.stringify(data.user))

      // Also complete the system-model-setting onboarding so the driver.js
      // tour never mounts. Without this, the tour starts 500ms after the
      // page loads and its full-screen SVG backdrop intercepts pointer
      // events for the rest of the test.
      const token = data.token
      await fetch(`/admin/graphql`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ query: onboardMutation, variables: { input: {} } }),
      }).catch(() => {})

      return { tokenLength: data.token.length }
    },
    {
      email: credentials.email,
      password: credentials.password,
      tokenKey: ACCESS_TOKEN_KEY,
      userKey: USER_INFO_KEY,
      onboardMutation:
        'mutation CompleteOnboarding($input: CompleteSystemModelSettingOnboardingInput!) { completeSystemModelSettingOnboarding(input: $input) }',
    }
  )
  console.log(`Seeded auth token (length=${result.tokenLength}) via direct API call`)
}


export async function signInAsAdmin(page: Page, credentials: AdminCredentials = defaultCredentials) {
  // Listen for console errors
  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      console.log('Browser console error:', msg.text())
    }
  })

  // Listen for page errors
  page.on('pageerror', (error) => {
    console.log('Page error:', error.message)
  })

  // Wait for the page to fully load
  await page.waitForLoadState('domcontentloaded', { timeout: 15000 })

  // Wait for React to mount - check for root element content
  try {
    await page.waitForFunction(
      () => {
        const root = document.getElementById('root')
        return root && root.innerHTML.length > 100
      },
      { timeout: 15000 }
    )
  } catch (error) {
    console.log('Warning: Root element may not be fully loaded')
    console.log('Page URL:', page.url())

    // Check if root exists at all
    const rootExists = await page.evaluate(() => {
      const root = document.getElementById('root')
      return { exists: !!root, innerHTML: root?.innerHTML.substring(0, 200) }
    })
    console.log('Root element state:', rootExists)
  }

  // Wait for the login form to be visible using reliable test IDs
  // Fallback to multiple selectors for backward compatibility
  const emailField = page
    .getByTestId('sign-in-email')
    .or(page.locator('input[type="email"], input[name="email"]'))
    .first()

  await emailField.waitFor({ state: 'visible', timeout: 20000 })

  // Fill in credentials with test IDs and fallback selectors
  const passwordField = page
    .getByTestId('sign-in-password')
    .or(page.locator('input[type="password"], input[name="password"]'))
    .first()

  await emailField.fill(credentials.email)
  await passwordField.fill(credentials.password)

  // Click login button - use test ID with fallback
  const loginButton = page.getByTestId('sign-in-submit').or(page.getByRole('button', { name: /登录|Sign In|Sign in/i }))
  await expect(loginButton).toBeVisible()

  // Wait for the sign-in API response before checking navigation
  const responsePromise = page.waitForResponse(
    (response) => response.url().includes('/admin/auth/signin') && response.status() === 200,
    { timeout: 15000 }
  )

  await loginButton.click()

  try {
    await responsePromise
  } catch (error) {
    console.log(`Sign-in API error: ${error}`)
    // Take a screenshot for debugging
    const timestamp = Date.now()
    await page.screenshot({ path: `test-results/sign-in-error-${timestamp}.png`, fullPage: true })
    console.log('Page URL:', page.url())
    throw error
  }

  // Wait for navigation away from sign-in page
  await page.waitForURL((url) => !url.toString().includes('/sign-in'), { timeout: 10000 })

  // Verify we're no longer on the sign-in page
  await expect(page.url()).not.toContain('/sign-in')
}

export async function ensureSignedIn(page: Page) {
  if (page.url().includes('/sign-in')) {
    await signInAsAdmin(page)
  }

  // Verify we have a valid token
  const hasToken = await page.evaluate(() => {
    const token = localStorage.getItem('axonhub_access_token')
    return !!token && token.length > 0
  })

  if (!hasToken) {
    console.warn('Warning: No valid auth token found, attempting to sign in')
    await signInAsAdmin(page)
  }
}

export async function gotoAndEnsureAuth(page: Page, path: string) {
  // Ensure we are on the app's origin before touching localStorage; about:blank
  // or data: URLs throw SecurityError when page.evaluate reads localStorage.
  if (page.url() === 'about:blank' || !page.url().startsWith('http')) {
    await page.goto('/', { waitUntil: 'domcontentloaded', timeout: 30000 })
  }

  // Fast path: if a valid token is already in localStorage, skip the sign-in flow.
  // Browser contexts are isolated per test, so a token here means this test
  // (or setup) already authenticated.
  const hasExistingToken = await page.evaluate((key) => {
    const token = localStorage.getItem(key)
    return !!token && token.length > 0
  }, ACCESS_TOKEN_KEY)

  if (!hasExistingToken) {
    // Seed the auth token directly via the sign-in API, bypassing the React form.
    //
    // The previous implementation drove the form via signInAsAdmin, but the
    // AuthGuard client-side redirect to /sign-in races with the 2s wait in
    // this function: on a cold Vite dev server the lazy-loaded /sign-in route
    // takes longer than 2s to compile and render, so the fallback
    // isVisible({ timeout: 2000 }) check returns false, needsLogin stays
    // false, the sign-in is skipped, and the page ends up on /sign-in with
    // no token in localStorage.
    //
    // Calling the API directly inside the page (via page.evaluate) writes
    // the token to localStorage synchronously, so the next page reload finds
    // it and the AuthGuard lets the request through.
    await seedAuthToken(page)
  }

  // Navigate to the target path. The store reads the token from localStorage
  // on module init, so the AuthGuard sees the token and does not redirect.
  await page.goto(path, { waitUntil: 'domcontentloaded', timeout: 30000 })

  // Wait for the app shell to render before returning. Use a short fixed
  // delay to let React mount and any lazy-loaded chunks arrive. The
  // models-onboarding-flow component starts a driver.js tour after a 500ms
  // timeout, so we need to wait long enough for it to mount before removing
  // the overlay below.
  await page.waitForTimeout(2500)

  // Strip any driver.js onboarding overlays and popovers that may be
  // intercepting pointer events. The overlays are full-screen SVGs rendered
  // above the app; they are normally dismissed by clicking
  // [data-settings-button], but on a cold Vite dev server the click handler
  // can race the overlay's mount and leave the backdrop behind. Removing the
  // elements directly is safe because the test environment does not depend
  // on the onboarding tour completing.
  await page.evaluate(() => {
    document
      .querySelectorAll('.driver-overlay, .driver-popover, #driver-popover-content')
      .forEach((el) => el.remove())
  }).catch(() => {})

  // Verify we have a valid token after login
  const hasToken = await page.evaluate((key) => {
    const token = localStorage.getItem(key)
    return !!token && token.length > 0
  }, ACCESS_TOKEN_KEY)

  if (!hasToken) {
    console.warn('Warning: No valid auth token found after login')
  }

  // Final wait for page to stabilize
  try {
    await page.waitForLoadState('networkidle', { timeout: 5000 })
  } catch (error) {
    // Ignore load state timeouts to avoid masking downstream assertions.
    console.log('Network idle timeout (expected in some cases)')
  }
}

export async function waitForGraphQLOperation(page: Page, operationName: string) {
  const lowerCamel = operationName.length
    ? operationName.charAt(0).toLowerCase() + operationName.slice(1)
    : operationName
  try {
    await Promise.race([
      page.waitForResponse((response) => {
        const url = response.url()
        const isGraphQL = url.includes('/admin/graphql') || url.includes('/graphql')
        if (!isGraphQL) return false
        const body = response.request().postData()
        if (!body) return false
        return body.includes(operationName) || body.includes(lowerCamel)
      }),
      // Fallback to a short timeout to avoid hard failures when backend is unavailable
      page.waitForTimeout(4000),
    ])
  } catch {
    // Swallow errors to keep tests resilient in environments without backend
  }
}
