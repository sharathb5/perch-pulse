import { test, expect } from '@playwright/test'
import { expectGraphReady, fixtures, mockApis, screenshotOpts, stabilize } from './helpers.js'

test.describe('Pulse intelligence on service graph (Milestone B)', () => {
  test('existing graph layout and selection remain intact with empty Pulse', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await expect(page.getByTestId('service-node-web')).toBeVisible()
    await expect(page.getByTestId('service-node-api')).toBeVisible()
    await expect(page.locator('.react-flow__edge')).toHaveCount(fixtures.graphOk.edges.length)

    await page.getByTestId('service-node-web').click()
    await expect(page).toHaveURL(/\/stack\/fixture-stack\/web$/)
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await expect(page.getByTestId('status-pill').first()).toBeVisible()
    await expect(page.getByTestId('pulse-section')).toBeVisible()
    await expect(page.getByTestId('pulse-empty')).toContainText('No Pulse mapping')
  })

  test('Pulse indicator appears for mapped services; recovered does not turn nodes red', async ({
    page,
  }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseIncidents: fixtures.pulseIncidentsHistorical,
    })
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    const apiNode = page.getByTestId('service-node-api')
    await expect(apiNode.getByTestId('pulse-indicator')).toBeVisible()
    await expect(apiNode.getByTestId('pulse-indicator')).toHaveAttribute('data-pulse-kind', 'historical')

    // Active probe health for api remains degraded (amber), not forced red by history.
    await expect(apiNode.getByTestId('status-pill')).toHaveAttribute('data-status', 'degraded')

    const webNode = page.getByTestId('service-node-web')
    await expect(webNode.getByTestId('pulse-indicator')).toHaveAttribute('data-pulse-kind', 'historical')
    await expect(webNode.getByTestId('status-pill')).toHaveAttribute('data-status', 'healthy')
  })

  test('unavailable Pulse intelligence is explicit, not healthy', async ({ page }) => {
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
    })
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    const db = page.getByTestId('service-node-db')
    await expect(db.getByTestId('pulse-indicator')).toHaveAttribute('data-pulse-kind', 'unavailable')
    // Probe health for db is still down from status.ok — Pulse must not rewrite it to healthy.
    await expect(db.getByTestId('status-pill')).toHaveAttribute('data-status', 'down')
  })

  test('selecting a service shows Pulse incidents and opens incident reference', async ({ page }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseIncidents: fixtures.pulseIncidentsHistorical,
    })
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await page.getByTestId('service-node-api').click()
    await expect(page.getByTestId('pulse-section')).toBeVisible()
    await expect(page.getByTestId('pulse-kind')).toHaveAttribute('data-pulse-kind', 'historical')
    await expect(page.getByTestId('pulse-incident-list')).toBeVisible()
    await expect(page.getByTestId('pulse-section')).toContainText('recovered')

    await page.getByTestId('pulse-incident-list').locator('button').first().click()
    await expect(page).toHaveURL(/incident=/)
    await expect(page.getByTestId('pulse-incident-ref')).toBeVisible()
    await expect(page.getByTestId('pulse-incident-ref')).toContainText('recovered')
    await expect(page.getByTestId('pulse-incident-ref')).toContainText('not proven causation')
  })

  test('Pulse API failure does not break the graph', async ({ page }) => {
    await mockApis(page, { pulseStatus: 503 })
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)

    await expect(page.getByTestId('service-node-web')).toBeVisible()
    await expect(page.getByTestId('error-banner')).toHaveCount(0)

    await page.getByTestId('service-node-web').click()
    await expect(page.getByTestId('detail-panel')).toBeVisible()
    await expect(page.getByTestId('pulse-error')).toContainText('Pulse unavailable')
  })

  test('empty Pulse data does not break the graph', async ({ page }) => {
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
    })
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)
    await expect(page.getByTestId('pulse-indicator')).toHaveCount(0)
    await expect(page.getByTestId('service-node-api')).toBeVisible()
  })

  test('graph demo banner remains honest when graph APIs fail; Pulse join disabled', async ({
    page,
  }) => {
    await mockApis(page, {
      graphStatus: 503,
      statusStatus: 503,
      pulseServices: fixtures.pulseServicesHistorical,
      pulseIncidents: fixtures.pulseIncidentsHistorical,
    })
    await page.goto('/stack/default')
    await expect(page.getByTestId('error-banner')).toContainText('last known or demo data')
    await expect(page.getByTestId('service-node-vercel')).toBeVisible()
    // Even if Pulse returns rows, demo/mock topology must not show Pulse indicators.
    await expect(page.getByTestId('pulse-indicator')).toHaveCount(0)
    await page.getByTestId('service-node-vercel').click()
    await expect(page.getByTestId('pulse-demo-disclaimer')).toBeVisible()
    await expect(page.getByTestId('pulse-empty')).toBeVisible()
  })

  test('Escape still closes the detail panel with Pulse section open', async ({ page }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseIncidents: fixtures.pulseIncidentsHistorical,
    })
    await page.goto('/stack/fixture-stack/api')
    await expect(page.getByTestId('pulse-section')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page).toHaveURL(/\/stack\/fixture-stack$/)
    await expect(page.getByTestId('detail-panel')).toHaveCount(0)
  })

  test('screenshot — selected node with Pulse historical info', async ({ page }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseIncidents: fixtures.pulseIncidentsHistorical,
    })
    await page.goto('/stack/fixture-stack/api')
    await expectGraphReady(page)
    await expect(page.getByTestId('pulse-kind')).toHaveAttribute('data-pulse-kind', 'historical')
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-selected-historical.png', screenshotOpts(page))
  })

  test('screenshot — default graph with Pulse historical indicators', async ({ page }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseIncidents: fixtures.pulseIncidentsHistorical,
    })
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)
    await expect(page.getByTestId('service-node-api').getByTestId('pulse-indicator')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-graph-historical.png', screenshotOpts(page))
  })

  test('screenshot — unavailable Pulse intelligence', async ({ page }) => {
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
    })
    await page.goto('/stack/fixture-stack/db')
    await expectGraphReady(page)
    await expect(page.getByTestId('pulse-kind')).toHaveAttribute('data-pulse-kind', 'unavailable')
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-unavailable.png', screenshotOpts(page))
  })

  test('screenshot — Pulse API error in detail panel', async ({ page }) => {
    await mockApis(page, { pulseStatus: 503 })
    await page.goto('/stack/fixture-stack/web')
    await expect(page.getByTestId('pulse-error')).toBeVisible()
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-api-error.png', screenshotOpts(page))
  })
})
