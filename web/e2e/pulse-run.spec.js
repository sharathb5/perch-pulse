import { expect, test } from '@playwright/test'
import { expectGraphReady, fixtures, mockApis, screenshotOpts, stabilize } from './helpers.js'

test.describe('Run Pulse (Milestone E)', () => {
  test('unavailable workflow shows clear reason', async ({ page }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseWorkflows: fixtures.pulseWorkflowsUnavailable,
    })
    await page.goto('/stack/fixture-stack?pulse=run')
    await expectGraphReady(page)
    await expect(page.getByTestId('pulse-tab-run')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('pulse-run-panel')).toBeVisible()
    await expect(
      page.getByTestId('pulse-run-unavailable-astronomy.recommendations-browse.v1'),
    ).toContainText('not configured')
    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-run-unavailable.png', screenshotOpts(page))
  })

  test('start diagnostic navigates to run id and polls without duplicate POST', async ({
    page,
  }) => {
    let posts = 0
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseWorkflows: fixtures.pulseWorkflowsAvailable,
      pulseRunCreate: fixtures.pulseRunCreate,
      pulseRun: fixtures.pulseRunCompletedGraph,
    })
    await page.route('**/api/pulse/runs', async (route) => {
      if (route.request().method() === 'POST') {
        posts += 1
        await route.fulfill({
          status: 202,
          contentType: 'application/json',
          body: JSON.stringify(fixtures.pulseRunCreate),
        })
        return
      }
      await route.continue()
    })

    await page.goto('/stack/fixture-stack?pulse=run')
    await expect(page.getByTestId('pulse-run-start-astronomy.recommendations-browse.v1')).toBeVisible()
    await page.getByTestId('pulse-run-start-astronomy.recommendations-browse.v1').click()
    await expect(page).toHaveURL(/pulse=run/)
    await expect(page).toHaveURL(/run=run_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/)
    await expect(page.getByTestId('pulse-run-status')).toHaveAttribute('data-status', 'completed')
    await expect(page.getByTestId('pulse-run-trace')).toBeVisible()
    // Allow a couple poll intervals — POST must remain 1.
    await page.waitForTimeout(3500)
    expect(posts).toBe(1)
  })

  test('completed run highlights graph nodes; clear removes them; incidents remain distinct', async ({
    page,
  }) => {
    await mockApis(page, {
      pulseServices: fixtures.pulseServicesHistorical,
      pulseWorkflows: fixtures.pulseWorkflowsAvailable,
      pulseRun: fixtures.pulseRunCompletedGraph,
    })
    await page.goto(
      '/stack/fixture-stack?pulse=run&run=run_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    )
    await expectGraphReady(page)
    await expect(page.getByTestId('pulse-run-detail')).toBeVisible()
    await expect(page.getByTestId('service-node-web')).toHaveAttribute('data-run-pulse', 'observed')
    await expect(page.getByTestId('service-node-api')).toHaveAttribute('data-run-pulse', 'error')
    await expect(page.getByTestId('service-node-db')).not.toHaveAttribute('data-run-pulse')
    await expect(page.getByTestId('pulse-run-unmapped')).toContainText('mystery-svc')

    // Historical incidents still available on Incidents tab.
    await page.getByTestId('pulse-tab-incidents').click()
    await expect(page.getByTestId('pulse-tab-incidents')).toHaveAttribute('aria-selected', 'true')
    await page.getByTestId('pulse-tab-run').click()

    await page.getByTestId('pulse-run-clear-highlights').click()
    await expect(page.getByTestId('service-node-web')).not.toHaveAttribute('data-run-pulse')
    await expect(page.getByTestId('service-node-api')).not.toHaveAttribute('data-run-pulse')

    await stabilize(page)
    await expect(page).toHaveScreenshot('pulse-run-completed.png', screenshotOpts(page))
  })

  test('failed run distinguishes HTTP 200 from incomplete trace', async ({ page }) => {
    await mockApis(page, {
      pulseWorkflows: fixtures.pulseWorkflowsAvailable,
      pulseRun: fixtures.pulseRunFailed,
    })
    await page.goto(
      '/stack/fixture-stack?pulse=run&run=run_dddddddddddddddddddddddddddddddd',
    )
    await expect(page.getByTestId('pulse-run-status')).toHaveAttribute('data-status', 'failed')
    await expect(page.getByTestId('pulse-run-failure')).toContainText('incomplete')
    await expect(page.getByTestId('pulse-run-request')).toContainText('200')
  })

  test('running state shows backend phase; API failure surfaces', async ({ page }) => {
    await mockApis(page, {
      pulseWorkflows: fixtures.pulseWorkflowsAvailable,
      pulseRun: fixtures.pulseRunRunning,
    })
    await page.goto(
      '/stack/fixture-stack?pulse=run&run=run_cccccccccccccccccccccccccccccccc',
    )
    await expect(page.getByTestId('pulse-run-phase')).toContainText('Waiting for trace')

    await mockApis(page, {
      pulseWorkflowsStatus: 503,
    })
    await page.goto('/stack/fixture-stack?pulse=run')
    await expect(page.getByTestId('pulse-run-workflows-error')).toBeVisible()
  })

  test('URL refresh restores run; Escape clears run then closes sidebar', async ({ page }) => {
    await mockApis(page, {
      pulseWorkflows: fixtures.pulseWorkflowsAvailable,
      pulseRun: fixtures.pulseRunCompletedGraph,
    })
    const url =
      '/stack/fixture-stack/web?pulse=run&run=run_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    await page.goto(url)
    await expect(page.getByTestId('pulse-run-detail')).toBeVisible()
    await page.reload()
    await expect(page.getByTestId('pulse-run-detail')).toBeVisible()
    await expect(page).toHaveURL(/\/web/)

    await page.keyboard.press('Escape')
    await expect(page).toHaveURL(/pulse=run/)
    await expect(page).not.toHaveURL(/[?&]run=/)
    // Close control (same path as Escape after run id is cleared).
    await page.getByTestId('pulse-sidebar-close').click()
    await expect(page).not.toHaveURL(/pulse=/)
    await expect(page.getByTestId('pulse-sidebar')).toHaveCount(0)
  })

  test('original graph without Run Pulse still works', async ({ page }) => {
    await mockApis(page)
    await page.goto('/stack/fixture-stack')
    await expectGraphReady(page)
    await expect(page.getByTestId('pulse-sidebar')).toHaveCount(0)
    await expect(page.getByTestId('service-node-web')).toBeVisible()
  })
})
