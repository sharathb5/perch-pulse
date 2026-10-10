import { test, expect } from '@playwright/test'
import { expectGraphReady, fixtures, mockApis, screenshotOpts, stabilize } from './helpers.js'

const historical = {
  pulseServices: fixtures.pulseServicesHistorical,
  pulseIncidents: fixtures.pulseIncidentsHistorical,
  pulseChanges: fixtures.pulseChanges,
  pulseIncidentDetail: fixtures.pulseIncidentDetail,
}

test.describe('Pulse sidebar (Milestone C)', () => {
  test('opens overview, incidents, changes; closes without graph reset', async ({ page }) => {
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await page.getByTestId('pulse-open').click()
    await expect(page).toHaveURL(/pulse=overview/)
    await expect(page.getByTestId('pulse-sidebar')).toBeVisible()
    await expect(page.getByTestId('pulse-overview')).toBeVisible()
    await expect(page.getByTestId('pulse-overview-counts')).toContainText('mapped services')
    await expect(page.getByTestId('pulse-overview-obs-unavailable')).toContainText(
      'process_memory_unavailable',
    )
    await expect(page.getByTestId('pulse-overview-no-metrics')).toBeVisible()

    await page.getByTestId('pulse-tab-incidents').click()
    await expect(page.getByTestId('pulse-incidents')).toBeVisible()
    await expect(page.getByTestId('pulse-incident-browser')).toBeVisible()

    await page.getByTestId('pulse-tab-changes').click()
    await expect(page.getByTestId('pulse-changes')).toBeVisible()
    await expect(page.getByTestId('pulse-change-simulated').first()).toContainText('simulated')

    await page.getByTestId('service-node-api').click()
    await expect(page).toHaveURL(/\/stack\/fixture-stack\/api/)
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await expect(page.getByTestId('pulse-sidebar')).toBeVisible()

    await expect(page).toHaveURL(/pulse=/)
    await page.getByTestId('pulse-sidebar-close').click()
    await expect(page.getByTestId('pulse-sidebar')).toHaveCount(0)
    await expect(page).toHaveURL(/\/stack\/fixture-stack\/api$/)
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await expect(page.getByTestId('service-node-api')).toBeVisible()
  })

  test('incident investigation shows digests, N/A, correlation, recovered historical', async ({
    page,
  }) => {
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack?pulse=incidents')
    await expect(page.getByTestId('pulse-incident-browser')).toBeVisible()

    await page.getByTestId('pulse-incident-browser').locator('button').first().click()
    await expect(page).toHaveURL(/incident=/)
    await expect(page.getByTestId('pulse-investigation')).toBeVisible()
    await expect(page.getByTestId('pulse-investigation-state')).toContainText('Recovered')
    await expect(page.getByTestId('pulse-investigation-historical')).toContainText('not a current outage')
    await expect(page.getByTestId('pulse-digest-latency_ms-during')).toContainText('820')
    await expect(page.getByTestId('pulse-digest-error_rate-after')).toContainText('N/A')
    await expect(page.getByTestId('pulse-investigation-observed')).toBeVisible()
    await expect(page.getByTestId('pulse-investigation-inferred')).toBeVisible()
    await expect(page.getByTestId('pulse-correlation-summary')).toContainText('not proven causation')
    await expect(page.getByTestId('pulse-investigation-correlation')).not.toContainText(
      'confirmed root cause',
    )
  })

  test('graph incident navigation opens sidebar; affected service selects node', async ({
    page,
  }) => {
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await page.getByTestId('service-node-api').click()
    await page.getByTestId('pulse-incident-list').locator('button').first().click()
    await expect(page.getByTestId('pulse-sidebar')).toBeVisible()
    await expect(page.getByTestId('pulse-investigation')).toBeVisible()

    const webLink = page.getByTestId(
      `pulse-affected-${encodeURIComponent('fixture/production/web')}`,
    )
    await expect(webLink).toBeVisible()
    await webLink.click()
    await expect(page).toHaveURL(/\/stack\/fixture-stack\/web/)
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await expect(page.getByTestId('pulse-sidebar')).toBeVisible()
  })

  test('browser history and Escape close sidebar before detail', async ({ page }) => {
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack/api')
    await page.getByTestId('pulse-open').click()
    await expect(page.getByTestId('pulse-sidebar')).toBeVisible()
    await page.getByTestId('pulse-tab-incidents').click()
    await page.getByTestId('pulse-incident-browser').locator('button').first().click()
    await expect(page.getByTestId('pulse-investigation')).toBeVisible()

    await page.goBack()
    await expect(page.getByTestId('pulse-incident-browser')).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(page.getByTestId('pulse-sidebar')).toHaveCount(0)
    await expect(page).toHaveURL(/\/stack\/fixture-stack\/api/)
    await expect(page.getByTestId('detail-panel')).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(page.getByTestId('detail-panel')).toHaveCount(0)
  })

  test('API error and empty data are handled; graph still works', async ({ page }) => {
    await mockApis(page, { pulseStatus: 503 })
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)
    await page.getByTestId('pulse-open').click()
    await expect(page.getByTestId('pulse-sidebar-error')).toContainText(/Pulse unavailable/)
    await expect(page.getByTestId('service-node-web')).toBeVisible()

    await page.goto('about:blank')
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesEmpty,
      pulseIncidents: {
        schema_version: 'pulse.api.incidents.v1',
        generated_at: '2026-10-10T12:00:00Z',
        data_sources: { observations: 'process_memory_unavailable', incidents: 'file_store' },
        count: 0,
        limit: 50,
        incidents: [],
      },
      pulseChanges: fixtures.pulseChangesEmpty,
    })
    await page.goto('/stack/fixture-stack?pulse=incidents')
    await expect(page.getByTestId('pulse-incidents-empty')).toBeVisible()
    await page.getByTestId('pulse-tab-changes').click()
    await expect(page.getByTestId('pulse-changes-empty')).toBeVisible()
  })

  test('narrow viewport keeps sidebar usable', async ({ page }) => {
    await page.setViewportSize({ width: 900, height: 800 })
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack/api?pulse=overview')
    await expect(page.getByTestId('pulse-sidebar')).toBeVisible()
    await expect(page.getByTestId('pulse-overview')).toBeVisible()
    await expect(page.getByTestId('perch-graph')).toBeVisible()
  })

  test('screenshot — Pulse overview', async ({ page }) => {
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack?pulse=overview')
    await expectGraphReady(page)
    await expect(page.getByTestId('pulse-overview')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-sidebar-overview.png', screenshotOpts(page))
  })

  test('screenshot — incident history', async ({ page }) => {
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack?pulse=incidents')
    await expect(page.getByTestId('pulse-incident-browser')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-sidebar-incidents.png', screenshotOpts(page))
  })

  test('screenshot — incident investigation', async ({ page }) => {
    await mockApis(page, historical)
    const id = encodeURIComponent(fixtures.pulseIncidentDetail.incident.incident_id)
    await page.goto(`/stack/fixture-stack/api?pulse=incidents&incident=${id}`)
    await expect(page.getByTestId('pulse-investigation')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-sidebar-investigation.png', screenshotOpts(page))
  })

  test('screenshot — change correlations tab', async ({ page }) => {
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack?pulse=changes')
    await expect(page.getByTestId('pulse-change-list')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-sidebar-changes.png', screenshotOpts(page))
  })

  test('screenshot — unavailable intelligence with sidebar', async ({ page }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesUnavailable,
      pulseIncidents: {
        schema_version: 'pulse.api.incidents.v1',
        generated_at: '2026-10-10T12:00:00Z',
        data_sources: { observations: 'process_memory_unavailable', incidents: 'file_store' },
        count: 0,
        limit: 50,
        incidents: [],
      },
      pulseChanges: fixtures.pulseChangesEmpty,
    })
    await page.goto('/stack/fixture-stack/db?pulse=overview')
    await expect(page.getByTestId('pulse-overview-obs-unavailable')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-sidebar-unavailable.png', screenshotOpts(page))
  })

  test('screenshot — narrow viewport', async ({ page }) => {
    await page.setViewportSize({ width: 900, height: 800 })
    await mockApis(page, historical)
    await page.goto('/stack/fixture-stack/api?pulse=overview')
    await expect(page.getByTestId('pulse-sidebar')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-sidebar-narrow.png', screenshotOpts(page))
  })
})
