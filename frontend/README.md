# TradeGrid Frontend

A minimal React + TypeScript application built with [Vite](https://vite.dev/guide/).
The current app displays TradeGrid branding. It is intentionally small so upcoming
frontend issues can build on it. No backend or environment variables are needed.
It does not yet include a TradingDesk, server connection, or manual Refresh
control. See the repository's [Developer Setup and Demo Guide](../docs/demo.md)
for the current application status and the fixture-defined trading scenarios.

## Prerequisites

Install **Node.js 22.12+ (22.x) or Node.js 24+** and npm, which comes with Node.js.
Use an LTS version of Node.js. Check your installation in a terminal:

```bash
node --version
npm --version
```

## Run locally

From the root of your cloned TradeGrid repository, enter the frontend directory:

```bash
cd frontend
```

Install the packages listed in `package.json`:

```bash
npm install
```

Start the development server:

```bash
npm run dev
```

Open the local URL printed in the terminal (usually `http://localhost:5173`).
Keep the command running while developing; saving source files updates the page.
Press **Ctrl+C** in the terminal to stop the server.

Run all commands below from `frontend/` as well.

## Checks and production build

```bash
npm run lint
npm run typecheck
npm test
npm run build
```

- `lint` runs Oxlint to check for common code and React mistakes.
- `typecheck` checks the application and Vite configuration with TypeScript.
- `test` runs the component tests once and exits non-zero if any fail. CI runs
  this command. See [Tests](#tests).
- `build` runs the same TypeScript checks, then creates a production bundle in
  `dist/`. Do not commit this generated directory.

To view the production build locally after building:

```bash
npm run preview
```

Open the URL printed in the terminal (usually `http://localhost:4173`). Stop it
with **Ctrl+C**. This is a local preview, not a production hosting service.

## Tests

Component tests use [Vitest](https://vitest.dev/) with
[React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)
in a simulated browser (jsdom). No backend or browser is needed.

```bash
npm test             # run every test once (what CI runs)
npm run test:watch   # rerun affected tests as you save files; press q to quit
npx vitest run src/components/TopNavigation.test.tsx   # run one file
```

Put a test next to the code it covers, named `*.test.tsx` (or `*.test.ts`), as in
`src/components/TopNavigation.test.tsx`. Write tests the way a person uses the
page:

- Find elements by role and accessible name, for example
  `screen.getByRole('button', { name: 'Orders' })`, rather than by CSS class.
- Interact with `userEvent` (clicks, typing, Tab, Enter) and await each action.
- Assert what a user or parent component can observe: visible text,
  `aria-current`, focus, and callback arguments from `vi.fn()`. Avoid snapshots.

`src/test/setup.ts` adds matchers such as `toBeInTheDocument` and
`toHaveAttribute` and clears the page after each test. Test configuration is in
`vitest.config.ts`, separate from `vite.config.ts`, so test tools never reach
the production bundle.

## Directory structure

```text
frontend/
  docs/app-shell.png Screenshot of the initial shell for review
  docs/top-navigation.png Desktop navigation screenshot for issue #10
  src/
    main.tsx         Mounts React into the HTML page
    App.tsx          Minimal app shell; compose future components here
    App.test.tsx     App-level navigation test
    App.css          App shell styles
    index.css        Global styles and basic browser defaults
    components/
      TopNavigation.tsx Reusable top navigation and its props
      TopNavigation.css Navigation styles and responsive layout
      TopNavigation.test.tsx Behavioral tests for the navigation
    test/
      setup.ts       Test setup: DOM matchers and cleanup
  index.html         HTML entry point and page title
  package.json       Dependencies and npm commands
  package-lock.json  Locked dependency versions; commit dependency updates
  vite.config.ts     Vite configuration with the React plugin
  vitest.config.ts   Test runner configuration (jsdom, setup file)
  tsconfig*.json     TypeScript settings for the app and build configuration
  .oxlintrc.json     Linter configuration
```

## Adding features

Create `src/components/` when adding the first reusable component, and keep its
CSS beside it. Import components into `App.tsx` to compose the interface. Use
`index.css` for global styles and `App.css` for the shell layout.

The top navigation from issue #10 is composed into the app shell. Issue #11 owns
the portfolio empty-state card; contributors can replace or extend the landing
content as those features arrive. Design-token documentation belongs to #15.

### Reusing the navigation

`TopNavigation` is controlled by its parent. Pass `activeItem` (Dashboard,
Portfolio, Orders, or Markets), an `onNavigate` callback, and optionally a formatted
`balance` string. The balance defaults to an em dash until data is available.



The app shell demonstrates local state changes; page content and URL routing can
be connected in later issues. The active button exposes `aria-current="page"`,
and the wordmark returns to Dashboard. Below 768px the navigation wraps to a
second row, with horizontal scrolling if its items exceed the available width.

Routing, state-management libraries, trading screens, and backend connections
are left for later issues.
