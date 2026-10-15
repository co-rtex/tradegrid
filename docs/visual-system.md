# TradeGrid Visual System

This document defines the initial visual system for the TradeGrid frontend.

The goal is to make independently built frontend components look and feel consistent across the platform. These guidelines provide a shared starting point for colors, spacing, typography, and common component styling.

---

## Colors

### Background and Surface Colors

TradeGrid uses a dark interface to keep the dashboard focused on trading data and reduce visual clutter.

| Purpose | Hex |
|---|---|
| Main background | `#0B1120` |
| Primary surface | `#111827` |
| Elevated surface | `#1F2937` |
| Border / divider | `#334155` |

### Usage

- `#0B1120` should be used for the main application background.
- `#111827` should be used for cards, tables, panels, and major content sections.
- `#1F2937` should be used for elevated surfaces such as dropdowns, modals, and hover states.
- `#334155` should be used for subtle borders and dividers.

Example:

```css
body {
  background-color: #0B1120;
}

.card {
  background-color: #111827;
  border: 1px solid #334155;
}
```

---

## Primary Brand / Accent Color

TradeGrid's primary accent color is cyan-blue.

| Purpose | Hex |
|---|---|
| Primary accent | `#22D3EE` |
| Accent hover | `#67E8F9` |
| Subtle accent background | `#164E63` |

The primary accent should be used for:

- Primary buttons
- Links
- Selected tabs
- Active navigation items
- Focus states
- Other non-financial interactive elements

Example:

```css
.primary-button {
  background-color: #22D3EE;
  color: #0B1120;
}

.primary-button:hover {
  background-color: #67E8F9;
}
```

Cyan-blue should be the default color for general interaction.

---

## Neutral Text Colors

| Purpose | Hex |
|---|---|
| Primary text | `#F8FAFC` |
| Secondary text | `#CBD5E1` |
| Muted text | `#94A3B8` |
| Disabled text | `#64748B` |

### Usage

Use primary text for:

- Page titles
- Important values
- Main labels
- Table values

Use secondary text for:

- Supporting information
- Descriptions
- Secondary labels

Use muted text for:

- Timestamps
- Helper text
- Table headers
- Less important metadata

---

## Semantic Financial Colors

### Positive / Buy

| Purpose | Hex |
|---|---|
| Positive / Buy | `#22C55E` |
| Positive background | `#14532D` |

Green is reserved for positive financial meaning.

Use green for:

- Buy orders
- Positive P&L
- Positive price movement
- Portfolio gains
- Positive percentage changes

Examples:

```text
BUY
+$245.20
+2.41%
```

### Negative / Sell

| Purpose | Hex |
|---|---|
| Negative / Sell | `#EF4444` |
| Negative background | `#7F1D1D` |

Red is reserved for negative financial meaning.

Use red for:

- Sell orders
- Negative P&L
- Negative price movement
- Portfolio losses
- Negative percentage changes

Examples:

```text
SELL
-$81.35
-1.27%
```

---

## Important Semantic Rule

Red and green are explicitly reserved for semantic financial meaning.

Do not use red or green as general decorative or interaction colors.

Correct usage:

```text
Buy order -> Green
Positive P&L -> Green
Positive price movement -> Green

Sell order -> Red
Negative P&L -> Red
Negative price movement -> Red

Submit button -> Cyan
Selected tab -> Cyan
Navigation link -> Cyan
Focused input -> Cyan
```

Avoid:

```text
Green Continue button
Red navigation icon
Green decorative border
Red selected tab
```

This keeps financial information consistent and easy to recognize across the application.

---

## Spacing

TradeGrid should use a consistent spacing scale based on multiples of 4px.

| Size | Value | Typical Use |
|---|---:|---|
| XS | `4px` | Small gaps and icons |
| SM | `8px` | Inline spacing |
| MD | `12px` | Small component padding |
| Base | `16px` | Standard spacing |
| LG | `24px` | Section spacing |
| XL | `32px` | Major layout gaps |
| 2XL | `48px` | Large page sections |

### Recommended Usage

```text
Button padding: 8px 16px
Input padding: 10px 12px
Card padding: 16px to 24px
Form field gap: 16px
Dashboard section gap: 24px to 32px
```

Contributors should prefer these spacing values instead of introducing arbitrary values.

---

## Border Radius

TradeGrid should use moderate corner rounding.

| Element | Radius |
|---|---:|
| Small tags / badges | `4px` |
| Inputs | `6px` |
| Buttons | `6px` |
| Cards / panels | `8px` |
| Modals | `10px` |

Example:

```css
.button {
  border-radius: 6px;
}

.card {
  border-radius: 8px;
}
```

Avoid excessive rounding unless the component specifically benefits from a pill-style shape, such as a status badge.

---

## Typography

TradeGrid should use a clean sans-serif font that remains readable in a data-heavy interface.

### Font Stack

```css
font-family:
  Inter,
  ui-sans-serif,
  system-ui,
  -apple-system,
  BlinkMacSystemFont,
  "Segoe UI",
  sans-serif;
```

If Inter is not available, the system font should be used.

### Type Scale

 **Usage**              | **Size** | **Weight** |
| --------------------- | -------- | ---------- |
| Page title            | `28px`   | 700        |
| Section heading       | `20px`   | 600        |
| Card heading          | `16px`   | 600        |
| Body text             | `16px`   | 400        |
| Table text            | `14px`   | 400        |
| Small text / metadata | `12px`   | 400        |

---

## Financial Numbers

Prices, balances, P&L, and percentages should use tabular numbers when possible.

Example:

```css
.financial-value {
  font-variant-numeric: tabular-nums;
}
```

This keeps numbers aligned in tables and prevents columns from visually shifting when values update.

For dense market data, a monospace font may also be used when appropriate.

Example:

```css
font-family:
  "JetBrains Mono",
  "SFMono-Regular",
  Consolas,
  monospace;
```

---

## Component Examples

### Primary Button

```text
Background: #22D3EE
Text: #0B1120
Border radius: 6px
Padding: 8px 16px
```

Use for general actions such as:

- Submit Order
- Save Settings
- Confirm

### Buy Button

```text
Background: #22C55E
Text: #0B1120
```

Green is used because the action represents a buy.

### Sell Button

```text
Background: #EF4444
Text: #0B1120
```

Red is used because the action represents a sell.

### Card

```text
Background: #111827
Border: 1px solid #334155
Border radius: 8px
Padding: 16px to 24px
```

Cards can be used for:

- Portfolio summaries
- Order entry
- Market data
- Open orders
- Recent executions

### Table

```text
Background: #111827
Primary text: #F8FAFC
Header text: #94A3B8
Row divider: #334155
Positive values: #22C55E
Negative values: #EF4444
```

Tables should prioritize readability over decoration.

---

## Quick Reference

```text
Main background: #0B1120
Primary surface: #111827
Elevated surface: #1F2937
Border / divider: #334155

Primary accent: #22D3EE
Accent hover: #67E8F9

Primary text: #F8FAFC
Secondary text: #CBD5E1
Muted text: #94A3B8
Disabled text: #64748B

Positive / Buy: #22C55E
Negative / Sell: #EF4444
```

---

## Contributor Guidelines

When building a new TradeGrid frontend component:

1. Use the defined background and surface colors.
2. Use cyan-blue for general interaction and navigation.
3. Use green only for positive or buy-related financial meaning.
4. Use red only for negative or sell-related financial meaning.
5. Follow the shared spacing scale.
6. Follow the shared border radius values.
7. Use the typography hierarchy defined above.
8. Prioritize readability and consistency over decoration.

These guidelines provide the initial visual standard for TradeGrid and can evolve as the frontend develops.
