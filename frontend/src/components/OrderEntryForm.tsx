import { useState } from "react";

function OrderEntryForm() {
    const [symbol, setSymbol] = useState("");
    const [side, setSide] = useState("Buy");
    const [quantity, setQuantity] = useState(0);
    const [price, setPrice] = useState(0);
    const [error, setError] = useState("");
    const [success, setSuccess] = useState("");
    const handleSubmit = () => {
        if (!symbol) {
            setError("Symbol is required");
            return;
        }

        if (quantity <=0) {
            setError("Quantity is required");
            return;
        }

        if (price<=0) {
            setError("Price is required");
            return;
        }

        setError("");
        setSuccess("");
        setSuccess(`Order submitted: ${side} ${quantity} ${symbol} at ${price}`);
    };
return (
    <div>
        <h1>Order Entry Form</h1>
        {error && <p style={{ color: "red" }}>{error}</p>}
        {success && <p style={{ color: "green" }}>{success}</p>}
        <label htmlFor="symbol">Symbol</label>
        <input 
        id="symbol"
        type="text"
        value={symbol}
        onChange={(e) => setSymbol(e.target.value)}
        />

        <label htmlFor="side">Buy/Sell</label>

        <select
        id="side"
        value={side}
        onChange={(e) => setSide(e.target.value)}
        >
            <option>Buy</option>
            <option>Sell</option>
        </select>
        <label htmlFor="quantity">Quantity</label>

        <input
        id="quantity"
        type="number"
        value={quantity}
        onChange={(e) => setQuantity(Number(e.target.value))}
        />

        <label htmlFor="price">Limit Price</label>
        <input
        id="price"
        type="number"
        value={price}
        onChange={(e) => setPrice(Number(e.target.value))}
        />

        <button onClick={handleSubmit}>Submit</button>
    </div>
    );
}

export default OrderEntryForm;
