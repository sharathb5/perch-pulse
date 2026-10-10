import { test, expect } from '@playwright/test'
import { expectGraphReady, fixtures, mockApis, screenshotOpts, stabilize } from './helpers.js'

test.describe('Perch viz graph (deterministic fixtures)', () => {
  test('renders fixture graph nodes and edges', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await expect(page.getByTestId('service-node-github')).toBeVisible()
    await expect(page.getByTestId('service-node-web')).toBeVisible()
    await expect(page.getByTestId('service-node-api')).toBeVisible()
    await expect(page.getByTestId('service-node-db')).toBeVisible()

    // React Flow edges render as SVG paths.
    const edgePaths = page.locator('.react-flow__edge')
    await expect(edgePaths).toHaveCount(fixtures.graphOk.edges.length)

    await expect(page.getByTestId('stack-name')).toHaveText('fixture-stack')
  })

  test('selecting a node opens the detail panel', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await page.getByTestId('service-node-web').click()
    await expect(page).toHaveURL(/\/stack\/fixture-stack\/web$/)
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await expect(page.getByTestId('detail-panel')).toContainText('web')

    await page.getByRole('button', { name: 'Close panel' }).click()
    await expect(page).toHaveURL(/\/stack\/fixture-stack$/)
    await expect(page.getByTestId('detail-panel')).toHaveCount(0)
  })

  test('navbar refresh and environment controls remain usable', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await expect(page.getByLabel('Environment')).toHaveValue('production')
    await page.getByLabel('Environment').selectOption('staging')
    await expect(page.getByLabel('Environment')).toHaveValue('staging')

    await page.getByRole('button', { name: 'Refresh' }).click()
    await expect(page.getByTestId('service-node-web')).toBeVisible()
  })

  test('API error keeps last-known (mock) data and shows banner', async ({ page }) => {
    await mockApis(page, { graphStatus: 503, statusStatus: 503 })
    await page.goto('/stack/default')
    await expectGraphReady(page)

    await expect(page.getByTestId('error-banner')).toBeVisible()
    await expect(page.getByTestId('error-banner')).toContainText('last known or demo data')

    // Documented mock seed: demo providers remain visible on total API failure.
    await expect(page.getByTestId('service-node-github')).toBeVisible()
    await expect(page.getByTestId('service-node-vercel')).toBeVisible()

    await page.getByRole('button', { name: 'Dismiss' }).click()
    await expect(page.getByTestId('error-banner')).toHaveCount(0)
  })

  test('Escape closes the detail panel', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack/web')
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page).toHaveURL(/\/stack\/fixture-stack$/)
    await expect(page.getByTestId('detail-panel')).toHaveCount(0)
  })

  test('unknown status is rendered for nodes missing from status payload', async ({ page }) => {
    await mockApis(page, {
      graph: fixtures.graphOk,
      status: {
        env: 'production',
        nodes: [{ name: 'web', provider: 'vercel', healthy: true, configured: true, error_rate: 0 }],
      },
    })
    await page.goto('/stack/fixture-stack')
    await expect(page.getByTestId('service-node-api')).toBeVisible()
    await expect(page.getByTestId('service-node-api').getByTestId('status-pill')).toHaveAttribute(
      'data-status',
      'unknown',
    )
  })

  test('empty API graph replaces mock with empty canvas', async ({ page }) => {
    await mockApis(page, {
      graph: fixtures.graphEmpty,
      status: fixtures.statusEmpty,
    })
    await page.goto('/stack/empty-stack')
    await expectGraphReady(page)

    await expect(page.getByTestId('service-node-github')).toHaveCount(0)
    await expect(page.getByTestId('service-node-web')).toHaveCount(0)
    await expect(page.locator('.react-flow__edge')).toHaveCount(0)
    await expect(page.getByTestId('stack-name')).toHaveText('empty-stack')
  })

  test('screenshot baselines — main graph', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)
    await expect(page.getByTestId('service-node-web')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('main-graph.png', screenshotOpts(page))
  })

  test('screenshot baselines — selected node + detail panel', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack/web')
    await expectGraphReady(page)
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('selected-node.png', screenshotOpts(page))
  })

  test('screenshot baselines — API error banner', async ({ page }) => {
    await mockApis(page, { graphStatus: 503, statusStatus: 503 })
    await page.goto('/stack/default')
    await expect(page.getByTestId('error-banner')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('api-error.png', screenshotOpts(page))
  })

  test('screenshot baselines — empty graph', async ({ page }) => {
    await mockApis(page, {
      graph: fixtures.graphEmpty,
      status: fixtures.statusEmpty,
    })
    await page.goto('/stack/empty-stack')
    await expectGraphReady(page)
    await stabilize(page)
    await expect(page).toHaveScreenshot('empty-graph.png', screenshotOpts(page))
  })

  test('screenshot baselines — narrow viewport', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockApis(page)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)
    await expect(page.getByTestId('service-node-web')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('main-graph-narrow.png', screenshotOpts(page))
  })
})
