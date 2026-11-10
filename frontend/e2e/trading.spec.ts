import { expect, test, type Browser, type BrowserContext, type Page } from '@playwright/test'
import { backendURL } from './ports.ts'

// Two-account smoke scenarios (#97) through the real UI, backend, REST, and
// WebSocket stream. They run in order from the seeded state of a backend
// started for this run (see playwright.config.ts), and assert the financial
// values a user sees, worked out by hand from docs/api.md and
// docs/portfolio.md. Every wait is for a visible condition, never a fixed
// sleep.

test.describe.configure({ mode: 'serial' })

const names = { alice: 'Alice (demo)', bob: 'Bob (demo)' } as const
type User = keyof typeof names

interface Window {
  context: BrowserContext
  page: Page
}

/** A separate browser window (its own context and storage) showing one account's desk. */
async function openDesk(browser: Browser, user: User): Promise<Window> {
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.goto('/')
  await page.getByLabel('Demo account').selectOption(user)
  await expect(page.getByRole('heading', { name: `Orders for ${names[user]}` })).toBeVisible()
  await expect(liveState(page)).toHaveText('Connected')
  return { context, page }
}

const liveState = (page: Page) => page.getByText('Live updates:').locator('strong')

/** The value next to a label in the desk's balances. */
const cash = (page: Page, user: User, label: 'Cash available' | 'Cash reserved' | 'Cash total') =>
  page.getByRole('region', { name: `${names[user]} balances` }).locator('dt', { hasText: label }).locator('+ dd')

const sharesRow = (page: Page, user: User, symbol: string) =>
  page
    .getByRole('region', { name: `${names[user]} balances` })
    .getByRole('row', { name: new RegExp(`^${symbol} `) })

async function placeOrder(page: Page, side: 'BUY' | 'SELL', quantity: string, price: string) {
  await page.getByRole('radio', { name: side }).check()
  await page.getByLabel('Quantity (shares)').fill(quantity)
  await page.getByLabel('Limit price (USD)').fill(price)
  await page.getByRole('button', { name: 'Submit limit order' }).click()
}

/** One amount in the portfolio's profit-and-loss breakdown. */
const pnlValue = (page: Page, label: 'Realized P&L' | 'Unrealized P&L' | 'Combined P&L') =>
  page
    .getByRole('region', { name: 'Profit and loss' })
    .locator('dt')
    .getByText(label, { exact: true })
    .locator('xpath=following-sibling::dd[1]')

const orderStatus = (page: Page) => page.getByRole('form', { name: 'Place a limit order' }).getByRole('status')

let alice: Window
let bob: Window

test.beforeAll(async ({ browser }) => {
  alice = await openDesk(browser, 'alice')
  bob = await openDesk(browser, 'bob')
})

test.afterAll(async () => {
  await alice?.context.close()
  await bob?.context.close()
})

test('Alice and Bob trade a full fill with price improvement, seen live in both windows', async () => {
  // Seed: $100,000.00 and 100 shares of each symbol per account.
  await expect(cash(alice.page, 'alice', 'Cash available')).toHaveText('$100,000.00')
  await expect(cash(bob.page, 'bob', 'Cash available')).toHaveText('$100,000.00')

  // Alice rests SELL 10 AAPL @ $190.00 (fixture 01): 10 shares reserved.
  await placeOrder(alice.page, 'SELL', '10', '190.00')
  await expect(orderStatus(alice.page)).toContainText('resting on the book with no fills')
  await expect(sharesRow(alice.page, 'alice', 'AAPL')).toHaveText(/AAPL\s*90 shares\s*10 shares\s*100 shares/)
  // Bob's window shows the new ask without pressing Refresh.
  await expect(bob.page.getByText('Best ask: $190.00')).toBeVisible()

  // Bob buys 10 @ $191.00 and fills at the resting $190.00 (fixture 02).
  await placeOrder(bob.page, 'BUY', '10', '191.00')
  await expect(orderStatus(bob.page)).toContainText('filled, 0 shares remaining. 1 fill: 10 shares at $190.00.')
  await expect(cash(bob.page, 'bob', 'Cash available')).toHaveText('$98,100.00')
  await expect(cash(bob.page, 'bob', 'Cash reserved')).toHaveText('$0.00')
  await expect(sharesRow(bob.page, 'bob', 'AAPL')).toHaveText(/AAPL\s*110 shares\s*0 shares\s*110 shares/)

  // Alice's window updates itself: proceeds received, reservation released.
  await expect(cash(alice.page, 'alice', 'Cash available')).toHaveText('$101,900.00')
  await expect(sharesRow(alice.page, 'alice', 'AAPL')).toHaveText(/AAPL\s*90 shares\s*0 shares\s*90 shares/)
  await expect(alice.page.getByRole('table', { name: `Orders for ${names.alice}` })).toContainText('Filled')
  for (const { page } of [alice, bob]) {
    await expect(page.getByText('No resting orders.')).toBeVisible()
    const trades = page.getByRole('table', { name: 'Recent executions · newest first' })
    await expect(trades.getByRole('row')).toHaveCount(2)
    await expect(trades.getByRole('row').nth(1)).toContainText(/AAPL\s*\$190\.00\s*10 shares/)
  }
})

test('a partial fill keeps the remainder and its reservation, and a risk rejection changes nothing', async () => {
  // Alice rests another SELL 10 @ $190.00; Bob buys 25 @ $190.50 (fixture 03).
  await placeOrder(alice.page, 'SELL', '10', '190.00')
  await expect(orderStatus(alice.page)).toContainText('resting on the book')
  await placeOrder(bob.page, 'BUY', '25', '190.50')
  await expect(orderStatus(bob.page)).toContainText('partially filled, 15 shares remaining. 1 fill: 10 shares at $190.00.')

  // Bob paid $1,900.00 and keeps 15 × $190.50 = $2,857.50 reserved.
  await expect(cash(bob.page, 'bob', 'Cash total')).toHaveText('$96,200.00')
  await expect(cash(bob.page, 'bob', 'Cash reserved')).toHaveText('$2,857.50')
  await expect(cash(bob.page, 'bob', 'Cash available')).toHaveText('$93,342.50')
  await expect(cash(alice.page, 'alice', 'Cash available')).toHaveText('$103,800.00')
  for (const { page } of [alice, bob]) {
    await expect(page.getByText('Best bid: $190.50')).toBeVisible()
    await expect(page.getByRole('table', { name: 'AAPL BUY / BID price levels' }).getByRole('row').nth(1)).toHaveText(
      /\$190\.50\s*15 shares\s*1/,
    )
  }
  const bobOrders = bob.page.getByRole('table', { name: `Orders for ${names.bob}` })
  await expect(bobOrders.getByRole('row').nth(1)).toContainText('Partially filled')

  // Bob tries to buy 600 @ $200.00 = $120,000.00 with $93,342.50 available.
  const ordersBefore = await bobOrders.getByRole('row').count()
  await placeOrder(bob.page, 'BUY', '600', '200.00')
  const form = bob.page.getByRole('form', { name: 'Place a limit order' })
  await expect(form.getByRole('alert')).toContainText('INSUFFICIENT_CASH')
  await expect(form.getByRole('alert')).toContainText('No order was created')
  await bob.page.getByRole('button', { name: 'Refresh' }).click()
  await expect(bob.page.getByText('Refreshing from the server…')).toBeHidden()
  await expect(cash(bob.page, 'bob', 'Cash available')).toHaveText('$93,342.50')
  await expect(cash(bob.page, 'bob', 'Cash reserved')).toHaveText('$2,857.50')
  await expect(bobOrders.getByRole('row')).toHaveCount(ordersBefore)
})

test('both portfolios update live after a cross-account fill at a price away from the reference', async ({ request }) => {
  for (const [user, { page }] of [['alice', alice], ['bob', bob]] as const) {
    await page.getByRole('navigation', { name: 'Primary navigation' }).getByRole('button', { name: 'Portfolio' }).click()
    await expect(page.getByRole('heading', { name: `${names[user]} portfolio` })).toBeVisible()
    await expect(page.getByLabel('Demo account')).toHaveValue(user)
    await expect(liveState(page)).toHaveText('Connected')
    // All trades so far were at the $190.00 reference price: equity is still the seed's.
    await expect(page.getByRole('region', { name: 'Portfolio summary' })).toContainText('$161,000.00')
  }

  // A third client trades through the REST API while both portfolios are open:
  // Alice sells 5 MSFT at $430.00 and Bob buys them ($420.00 reference price).
  for (const order of [
    { userId: 'alice', symbol: 'MSFT', side: 'SELL', type: 'LIMIT', price: 43000, quantity: 5 },
    { userId: 'bob', symbol: 'MSFT', side: 'BUY', type: 'LIMIT', price: 43000, quantity: 5 },
  ]) {
    const response = await request.post(`${backendURL}/api/v1/orders`, { data: order })
    expect(response.status()).toBe(201)
  }

  // Alice: +$2,150.00 cash, realizes $2,150.00 - $2,100.00 basis = +$50.00.
  // Equity $105,950.00 + 80 × $190.00 + 95 × $420.00 = $161,050.00.
  const aliceSummary = alice.page.getByRole('region', { name: 'Portfolio summary' })
  await expect(aliceSummary).toContainText('$161,050.00')
  await expect(pnlValue(alice.page, 'Realized P&L')).toHaveText('+$50.00 Gain')
  await expect(alice.page.getByRole('table', { name: 'Positions' }).getByRole('row', { name: /^MSFT/ })).toContainText(
    /MSFT\s*95 shares/,
  )

  // Bob: basis $42,000.00 + $2,150.00 for 105 MSFT worth $44,100.00: -$50.00 unrealized.
  const bobSummary = bob.page.getByRole('region', { name: 'Portfolio summary' })
  await expect(bobSummary).toContainText('$160,950.00')
  await expect(pnlValue(bob.page, 'Unrealized P&L')).toHaveText('-$50.00 Loss')
  await expect(bob.page.getByRole('table', { name: 'Positions' }).getByRole('row', { name: /^MSFT/ })).toContainText(
    /MSFT\s*105 shares/,
  )
  // Combined P&L across both accounts is zero: value only moved between them.
  await expect(pnlValue(alice.page, 'Combined P&L')).toHaveText('+$50.00 Gain')
  await expect(pnlValue(bob.page, 'Combined P&L')).toHaveText('-$50.00 Loss')
})

test('switching accounts quickly never shows the previous account’s portfolio', async () => {
  const { page } = alice
  const select = page.getByLabel('Demo account')
  for (const user of ['bob', 'alice', 'bob', 'alice', 'bob'] as const) await select.selectOption(user)
  await expect(page.getByRole('heading', { name: `${names.bob} portfolio` })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Portfolio summary' })).toContainText('$160,950.00')
  await expect(page.getByRole('region', { name: 'Portfolio summary' })).not.toContainText('$161,050.00')
  await select.selectOption('alice')
  await expect(page.getByRole('region', { name: 'Portfolio summary' })).toContainText('$161,050.00')
})
