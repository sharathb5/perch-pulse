import { expect, test } from '@playwright/test'
import { fixtures, mockApis, screenshotOpts } from './helpers.js'

test.describe('Live Pulse UI', () => {
  test('disabled live telemetry shows unavailable, not healthy; graph unchanged', async ({
    page,
  }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesEmpty,
      pulseLiveStatus: fixtures.pulseLiveStatusDisabled,
    })
    await page.goto('/stack/fixture-stack?pulse=overview')
    await expect(page.getByTestId('pulse-live-telemetry')).toBeVisible()
    await expect(page.getByTestId('pulse-live-runtime')).toHaveAttribute(
      'data-live-state',
      'disabled',
    )
    await expect(page.getByTestId('pulse-live-runtime')).toContainText(/disabled/i)
    await expect(page.getByTestId('pulse-live-runtime')).not.toContainText(/healthy/i)
    // StatusPill path still independent — no live regression attrs.
    await expect(page.getByTestId('service-node-web')).not.toHaveAttribute('data-live-pulse')
  })

  test('live regression indicator on mapped graph node; StatusPill untouched', async ({
    page,
  }) => {
    // Remap Go fixture payment → fixture graph node "api".
    const liveServices = structuredClone(fixtures.pulseLiveServicesRegression)
    liveServices.services[0].graph_node = 'api'
    const liveIncidents = structuredClone(fixtures.pulseLiveIncidentsActive)
    if (liveIncidents.incidents?.[0]) {
      liveIncidents.incidents[0].graph_node = 'api'
    }

    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseIncidents: fixtures.pulseIncidentsHistorical,
      pulseLiveStatus: fixtures.pulseLiveStatusCollecting,
      pulseLiveServices: liveServices,
      pulseLiveIncidents: liveIncidents,
    })
    await page.goto('/stack/fixture-stack?pulse=overview')
    await expect(page.getByTestId('pulse-live-runtime')).toHaveAttribute(
      'data-live-state',
      'collecting',
    )
    await expect(page.getByTestId('pulse-live-regressions')).toHaveText('1')
    await expect(page.getByTestId('service-node-api')).toHaveAttribute(
      'data-live-pulse',
      'regression',
    )
    await expect(page.getByTestId('service-node-api').getByTestId('pulse-indicator')).toHaveAttribute(
      'data-pulse-kind',
      'live_regression',
    )
    // Probe StatusPill remains present and independent.
    await expect(page.getByTestId('service-node-api').locator('[data-testid="status-pill"]')).toBeVisible()

    await page.getByTestId('pulse-tab-incidents').click()
    await expect(page.getByTestId('pulse-incident-lifecycle').first()).toBeVisible()

    await page.getByTestId('pulse-tab-run').click()
    await expect(page.getByTestId('pulse-run-panel')).toBeVisible()

    await page.getByTestId('pulse-tab-overview').click()
    await expect(page.getByTestId('pulse-live-telemetry')).toBeVisible()
    await expect(page).toHaveScreenshot('pulse-live-regression.png', screenshotOpts(page))
  })

  test('warming up copy; no regression claim from empty baseline', async ({ page }) => {
    await mockApis(page, {
      pulseLiveStatus: fixtures.pulseLiveStatusWarming,
      pulseLiveServices: {
        schema_version: 'pulse.api.live_services.v1',
        generated_at: fixtures.pulseLiveStatusWarming.generated_at,
        count: 0,
        services: [],
        status: fixtures.pulseLiveStatusWarming.status,
        limitations: [],
      },
    })
    await page.goto('/stack/fixture-stack?pulse=overview')
    const state = await page.getByTestId('pulse-live-runtime').getAttribute('data-live-state')
    expect(['warming_up', 'collecting']).toContain(state)
    await expect(page.getByTestId('pulse-live-runtime-detail')).toBeVisible()
  })

  test('polling refresh does not reset pulse query navigation', async ({ page }) => {
    let liveStatusHits = 0
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseLiveStatus: fixtures.pulseLiveStatusCollecting,
      pulseLiveServices: fixtures.pulseLiveServicesRegression,
    })
    await page.route('**/api/pulse/live/status', async (route) => {
      liveStatusHits += 1
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(fixtures.pulseLiveStatusCollecting),
      })
    })
    await page.goto('/stack/fixture-stack/api?pulse=incidents')
    await expect(page.getByTestId('pulse-tab-incidents')).toHaveAttribute('aria-selected', 'true')
    // Wait for a second poll cycle (~8s) without navigation reset.
    await expect.poll(() => liveStatusHits, { timeout: 20_000 }).toBeGreaterThanOrEqual(2)
    await expect(page).toHaveURL(/pulse=incidents/)
    await expect(page.getByTestId('pulse-tab-incidents')).toHaveAttribute('aria-selected', 'true')
  })
})
