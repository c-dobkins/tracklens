import { expect, test, type Page } from '@playwright/test'

// Fail any test that logs an error, including CSP violations.
function collectErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text())
  })
  page.on('pageerror', (e) => errors.push(String(e)))
  return errors
}

test('shows the sample period end to end', async ({ page }) => {
  const errors = collectErrors(page)
  await page.goto('/')

  await expect(page.getByTestId('total-hours')).toHaveText('5.75 h')
  await expect(page.getByText('+0.75 h vs the previous 7 days')).toBeVisible()
  await expect(page.getByText('3 to review')).toBeVisible()

  // Each chart drew its bars. Every sample project is wholly billable or wholly
  // not, so the zero segments draw nothing: 4 project bars, 4 changes, and two
  // days with both segments.
  await expect(page.getByRole('region', { name: 'Hours by project' }).locator('g.data-bar')).toHaveCount(4)
  await expect(page.getByRole('region', { name: 'Hours per day' }).locator('g.data-bar')).toHaveCount(4)
  await expect(page.getByRole('region', { name: 'Change by project' }).locator('g.data-bar')).toHaveCount(4)
  await expect(page.getByRole('region', { name: 'Data checks' }).getByRole('listitem')).toHaveCount(3)
  expect(errors).toEqual([])
})

test('keyboard focus on a bar shows its tooltip', async ({ page }) => {
  await page.goto('/')
  await page.getByTestId('bar.Billable.Client portal').focus()
  await expect(page.locator('.tooltip')).toContainText('3.25 h')
  await expect(page.locator('.tooltip')).toContainText('Billable · Client portal')
})

test('filters scope every view', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByTestId('total-hours')).toHaveText('5.75 h')

  await page.getByRole('searchbox', { name: 'Project' }).fill('portal')
  await expect(page.getByTestId('total-hours')).toHaveText('3.25 h')
  await expect(page.getByText('1 to review')).toBeVisible() // entries 101 and 102 overlap

  await page.getByLabel('Last 30 days').check()
  const days = page.getByRole('region', { name: 'Hours per day' })
  await days.getByText('View as table').click()
  await expect(days.getByRole('row')).toHaveCount(1 + 30) // header + every day, including empty ones
})

test('dark mode renders without errors', async ({ page }) => {
  const errors = collectErrors(page)
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto('/')
  await expect(page.getByTestId('total-hours')).toBeVisible()
  const background = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  expect(background).toBe('rgb(13, 13, 13)')
  expect(errors).toEqual([])
})

test('fits a phone screen without sideways scrolling', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/')
  await expect(page.getByTestId('total-hours')).toBeVisible()
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
})
