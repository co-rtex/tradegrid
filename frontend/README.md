# TradeGrid Frontend

A minimal React + TypeScript application built with [Vite](https://vite.dev/guide/).
The current app displays TradeGrid branding. It is intentionally small so upcoming
frontend issues can build on it. No backend or environment variables are needed.

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
npm run build
```

- `lint` runs Oxlint to check for common code and React mistakes.
- `typecheck` checks the application and Vite configuration with TypeScript.
- `build` runs the same TypeScript checks, then creates a production bundle in
  `dist/`. Do not commit this generated directory.

To view the production build locally after building:

```bash
npm run preview
```

Open the URL printed in the terminal (usually `http://localhost:4173`). Stop it
with **Ctrl+C**. This is a local preview, not a production hosting service.

There is no automated test suite yet. For this scaffold, run the checks above
and verify that the TradeGrid heading renders in your browser. Tests can be added
alongside future interactive features.

## Directory structure

```text
frontend/
  docs/app-shell.png Screenshot of the initial shell for review
  src/
    main.tsx         Mounts React into the HTML page
    App.tsx          Minimal app shell; compose future components here
    App.css          App shell styles
    index.css        Global styles and basic browser defaults
  index.html         HTML entry point and page title
  package.json       Dependencies and npm commands
  package-lock.json  Locked dependency versions; commit dependency updates
  vite.config.ts     Vite configuration with the React plugin
  tsconfig*.json     TypeScript settings for the app and build configuration
  .oxlintrc.json     Linter configuration
```

## Adding features

Create `src/components/` when adding the first reusable component, and keep its
CSS beside it. Import components into `App.tsx` to compose the interface. Use
`index.css` for global styles and `App.css` for the shell layout.

Issue #10 owns the top navigation component, and #11 owns the portfolio
empty-state card. Neither is included here; contributors can replace or extend
the landing content as those features arrive. Design-token documentation belongs
to #15. The current plain CSS is only enough to establish basic branding.

Routing, state-management libraries, trading screens, and backend connections
are left for later issues.
