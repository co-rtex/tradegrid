import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import OrderEntryForm, {
  type OrderEntryValues,
} from '../components/OrderEntryForm'
import '../index.css'

function Preview() {
  const [value, onChange] = useState<OrderEntryValues>({
    symbol: 'AAPL',
    side: 'BUY',
    quantity: '25',
    price: '190.50',
  })
  const [success, setSuccess] = useState('')

  return (
    <main style={{ padding: 24 }}>
      <OrderEntryForm
        userId="bob"
        value={value}
        onChange={onChange}
        success={success}
        onSubmit={(request) => {
          console.log('Callback payload:', request)
          setSuccess('Order accepted by mock callback.')
        }}
      />
    </main>
  )
}

createRoot(document.getElementById('root')!).render(<Preview />)