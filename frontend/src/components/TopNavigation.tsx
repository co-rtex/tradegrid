import './TopNavigation.css'

const navigationItems = ['Dashboard', 'Portfolio', 'Orders', 'Markets'] as const
export type NavigationItem = (typeof navigationItems)[number]

export interface TopNavigationProps {
  activeItem: NavigationItem
  onNavigate: (item: NavigationItem) => void
  balance?: string
}

export default function TopNavigation({
  activeItem,
  onNavigate,
  balance = '—',
}: TopNavigationProps) {
  return (
    <header className="top-navigation">
      <div className="top-navigation-inner">
        <button
          className="tradegrid-wordmark"
          type="button"
          onClick={() => onNavigate('Dashboard')}
          aria-label="TradeGrid dashboard"
        >
          Trade<span>Grid</span>
        </button>
        <nav aria-label="Primary navigation">
          <ul className="navigation-items">
            {navigationItems.map((item) => (
              <li key={item}>
                <button
                  type="button"
                  className="navigation-item"
                  aria-current={activeItem === item ? 'page' : undefined}
                  onClick={() => onNavigate(item)}
                >
                  {item}
                </button>
              </li>
            ))}
          </ul>
        </nav>
        <div className="navigation-balance">
          <span className="balance-label">Balance</span>
          <span className="balance-value">{balance}</span>
        </div>
      </div>
    </header>
  )
}
