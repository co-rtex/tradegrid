import { expect, test, type Page } from '@playwright/test'

// Responsive and keyboard smoke checks (#93) on the live app. They only read,
// so they do not depend on the trading scenarios' state.

const viewports = [
  { name: 'phone', width: 375, height: 812 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1280, height: 800 },
]

/** The page itself must not scroll sideways; wide tables scroll inside their own region. */
async function expectNoHorizontalPageScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow, 'horizontal page overflow in px').toBeLessThanOrEqual(0)
}

for (const viewport of viewports) {
  test(`no horizontal page overflow on the desk and portfolio at ${viewport.width}px (${viewport.name})`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: /^Orders for / })).toBeVisible()
    await expect(page.getByText('Best bid:')).toBeVisible()
    await expectNoHorizontalPageScroll(page)

    await page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('button', { name: 'Portfolio' }).click()
    await expect(page.getByRole('region', { name: 'Portfolio summary' })).toBeVisible()
    await expectNoHorizontalPageScroll(page)
  })
}

test('every desk control can be reached with the keyboard, with a visible focus indicator', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /^Orders for / })).toBeVisible()

  const wanted = ['Demo account', 'Market', 'Refresh', 'Symbol', 'Quantity (shares)', 'Limit price (USD)', 'Submit limit order']
  const reached = new Set<string>()
  const unfocusedOutline = new Set<string>()
  for (let i = 0; i < 60 && reached.size < wanted.length; i++) {
    await page.keyboard.press('Tab')
    const focused = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null
      if (!el || el === document.body) return null
      const labelled = el.id ? document.querySelector(`label[for="${el.id}"]`)?.textContent : null
      const wrapping = el.closest('label')?.querySelector('span')?.textContent
      const name = el.getAttribute('aria-label') ?? labelled ?? wrapping ?? el.textContent ?? ''
      const style = getComputedStyle(el)
      return { name: name.trim(), outline: style.outlineStyle !== 'none' && style.outlineWidth !== '0px', shadow: style.boxShadow !== 'none' }
    })
    if (!focused) continue
    const match = wanted.find((name) => focused.name === name)
    if (!match) continue
    reached.add(match)
    if (!focused.outline && !focused.shadow) unfocusedOutline.add(match)
  }
  expect([...reached].sort()).toEqual([...wanted].sort())
  expect([...unfocusedOutline], 'controls without a visible focus style').toEqual([])
})
